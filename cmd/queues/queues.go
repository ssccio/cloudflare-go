// Package queues implements the `cf queues` subcommand group: the account's
// Queues and their backlog over time.
package queues

import (
	"fmt"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/queues"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/graphql"
)

// Cmd is the `cf queues` parent command.
var Cmd = &cobra.Command{
	Use:   "queues",
	Short: "Read Queues and their backlog",
	Long: `List the account's Queues and show each queue's backlog by hour.

Needs Account → Queues → Read for list, and Account → Account Analytics →
Read for backlog.`,
}

var account string

func init() {
	Cmd.PersistentFlags().StringVar(&account, "account", "", "Account ID (or CLOUDFLARE_ACCOUNT_ID)")
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(backlogCmd)
}

type queueRow struct {
	Name      string   `json:"name"      toon:"name"`
	ID        string   `json:"id"        toon:"id"`
	Consumers []string `json:"consumers" toon:"consumers"`
	Producers int64    `json:"producers" toon:"producers"`
	Modified  string   `json:"modified"  toon:"modified"`
}

func listQueues(cmd *cobra.Command, c *cmdutil.Context) ([]queues.Queue, error) {
	page, err := c.Client.Queues.List(cmd.Context(), queues.QueueListParams{AccountID: cf.F(c.AccountID)})
	if err != nil {
		return nil, err
	}
	return page.Result, nil
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List the account's Queues",
	Long: `List the account's Queues with their consumers and producer count.

Examples:
  cf queues list --account ACCOUNT_ID`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		qs, err := listQueues(cmd, c)
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		rows := []queueRow{}
		for _, q := range qs {
			cons := []string{}
			for _, k := range q.Consumers {
				cons = append(cons, fmt.Sprintf("%s:%s", k.Type, k.Script))
			}
			rows = append(rows, queueRow{Name: q.QueueName, ID: q.QueueID, Consumers: cons,
				Producers: int64(q.ProducersTotalCount), Modified: q.ModifiedOn})
		}
		if p.JSON || p.TOON {
			p.PrintResult(rows)
			return nil
		}
		if len(rows) == 0 {
			p.Info("No queues in account %s.", c.AccountID)
			return nil
		}
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			table = append(table, []string{r.Name, r.ID, strings.Join(r.Consumers, ","), fmt.Sprint(r.Producers), r.Modified})
		}
		p.Table([]string{"NAME", "ID", "CONSUMERS", "PRODUCERS", "MODIFIED"}, table)
		return nil
	},
}

var (
	backlogHours int
	backlogQueue string
)

type backlogRow struct {
	Queue    string  `json:"queue"    toon:"queue"`
	Hour     string  `json:"hour"     toon:"hour"`
	Messages float64 `json:"messages" toon:"messages"`
	Bytes    float64 `json:"bytes"    toon:"bytes"`
}

const backlogQuery = `query($account: String!, $since: Time!, $until: Time!) {
  viewer { accounts(filter: {accountTag: $account}) {
    queueBacklogAdaptiveGroups(limit: 10000, filter: {datetime_geq: $since, datetime_leq: $until},
      orderBy: [datetimeHour_ASC]) {
      dimensions { queueId datetimeHour }
      avg { messages bytes }
    }
  } }
}`

var backlogCmd = &cobra.Command{
	Use:   "backlog",
	Short: "Show each queue's average backlog by hour",
	Long: `Show the average number of messages waiting in each queue, by hour. A
backlog that only grows means the consumer is failing or too slow. A dead
letter queue with any backlog holds messages that ran out of retries.

Examples:
  cf queues backlog --account ACCOUNT_ID --hours 48
  cf queues backlog --account ACCOUNT_ID --queue waf-logpush-dlq --json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		qs, err := listQueues(cmd, c)
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		names := map[string]string{}
		for _, q := range qs {
			names[q.QueueID] = q.QueueName
		}

		since, until := graphql.Window(backlogHours)
		var resp struct {
			Data struct {
				Viewer struct {
					Accounts []struct {
						Groups []struct {
							Dimensions struct {
								QueueID string `json:"queueId"`
								Hour    string `json:"datetimeHour"`
							} `json:"dimensions"`
							Avg struct {
								Messages float64 `json:"messages"`
								Bytes    float64 `json:"bytes"`
							} `json:"avg"`
						} `json:"queueBacklogAdaptiveGroups"`
					} `json:"accounts"`
				} `json:"viewer"`
			} `json:"data"`
			Errors []graphql.Error `json:"errors"`
		}
		vars := map[string]any{"account": c.AccountID, "since": since, "until": until}
		if err := graphql.Do(cmd.Context(), c.Token, backlogQuery, vars, &resp); err != nil {
			p.Error("GraphQL error: %v", err)
			return err
		}
		if err := graphql.CheckErrors(resp.Errors); err != nil {
			p.Error("GraphQL error: %v (backlog needs Account Analytics → Read)", err)
			return err
		}

		rows := []backlogRow{}
		for _, a := range resp.Data.Viewer.Accounts {
			for _, g := range a.Groups {
				name := names[g.Dimensions.QueueID]
				if name == "" {
					name = g.Dimensions.QueueID
				}
				if backlogQueue != "" && name != backlogQueue && g.Dimensions.QueueID != backlogQueue {
					continue
				}
				rows = append(rows, backlogRow{Queue: name, Hour: g.Dimensions.Hour, Messages: g.Avg.Messages, Bytes: g.Avg.Bytes})
			}
		}
		if p.JSON || p.TOON {
			p.PrintResult(rows)
			return nil
		}
		if len(rows) == 0 {
			p.Info("No backlog samples in the last %dh.", backlogHours)
			return nil
		}
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			table = append(table, []string{r.Queue, r.Hour, fmt.Sprintf("%.0f", r.Messages), fmt.Sprintf("%.0f", r.Bytes)})
		}
		p.Table([]string{"QUEUE", "HOUR", "MESSAGES", "BYTES"}, table)
		return nil
	},
}

func init() {
	backlogCmd.Flags().IntVar(&backlogHours, "hours", 24, "Look-back window in hours")
	backlogCmd.Flags().StringVar(&backlogQueue, "queue", "", "Only this queue (name or ID)")
}
