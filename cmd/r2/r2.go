// Package r2 implements the `cf r2` subcommand group: read-only views of an
// R2 bucket's event notifications and lifecycle rules.
package r2

import (
	"fmt"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/r2"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

// Cmd is the `cf r2` parent command.
var Cmd = &cobra.Command{
	Use:   "r2",
	Short: "Read R2 bucket configuration",
	Long: `Read an R2 bucket's event notification rules and lifecycle rules.

Needs Account → Workers R2 Storage → Read on the token.`,
}

var (
	account string
	bucket  string
)

func init() {
	Cmd.PersistentFlags().StringVar(&account, "account", "", "Account ID (or CLOUDFLARE_ACCOUNT_ID)")
	Cmd.PersistentFlags().StringVar(&bucket, "bucket", "", "Bucket name (required)")
	_ = Cmd.MarkPersistentFlagRequired("bucket")
	Cmd.AddCommand(notificationsCmd)
	Cmd.AddCommand(lifecycleCmd)
}

// NotificationRow is one event notification rule.
type NotificationRow struct {
	Queue     string   `json:"queue"      toon:"queue"`
	QueueID   string   `json:"queue_id"   toon:"queue_id"`
	RuleID    string   `json:"rule_id"    toon:"rule_id"`
	Prefix    string   `json:"prefix"     toon:"prefix"`
	Suffix    string   `json:"suffix"     toon:"suffix"`
	Actions   []string `json:"actions"    toon:"actions"`
	CreatedAt string   `json:"created_at" toon:"created_at"`
}

var notificationsCmd = &cobra.Command{
	Use:   "notifications",
	Short: "List a bucket's event notification rules",
	Long: `List the bucket's event notification rules: which queue each rule sends to,
and the prefix and suffix it filters on. An empty prefix and suffix means
every object in the bucket triggers the rule.

Examples:
  cf r2 notifications --account ACCOUNT_ID --bucket my-bucket`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		res, err := c.Client.R2.Buckets.EventNotifications.List(cmd.Context(), bucket,
			r2.BucketEventNotificationListParams{AccountID: cf.F(c.AccountID)})
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		rows := []NotificationRow{}
		for _, q := range res.Queues {
			for _, r := range q.Rules {
				acts := make([]string, 0, len(r.Actions))
				for _, a := range r.Actions {
					acts = append(acts, string(a))
				}
				rows = append(rows, NotificationRow{Queue: q.QueueName, QueueID: q.QueueID, RuleID: r.RuleID,
					Prefix: r.Prefix, Suffix: r.Suffix, Actions: acts, CreatedAt: r.CreatedAt})
			}
		}
		if p.JSON || p.TOON {
			p.PrintResult(rows)
			return nil
		}
		if len(rows) == 0 {
			p.Info("No event notification rules on bucket %s.", bucket)
			return nil
		}
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			table = append(table, []string{r.Queue, r.RuleID, r.Prefix, r.Suffix, strings.Join(r.Actions, ","), r.CreatedAt})
		}
		p.Table([]string{"QUEUE", "RULE_ID", "PREFIX", "SUFFIX", "ACTIONS", "CREATED"}, table)
		return nil
	},
}

// LifecycleRow is one lifecycle rule.
type LifecycleRow struct {
	ID            string `json:"id"             toon:"id"`
	Enabled       bool   `json:"enabled"        toon:"enabled"`
	Prefix        string `json:"prefix"         toon:"prefix"`
	DeleteObjects string `json:"delete_objects" toon:"delete_objects"`
	AbortUploads  string `json:"abort_uploads"  toon:"abort_uploads"`
}

var lifecycleCmd = &cobra.Command{
	Use:   "lifecycle",
	Short: "List a bucket's lifecycle rules",
	Long: `List the bucket's lifecycle rules. DELETE_OBJECTS empty means the rule
never expires objects.

Examples:
  cf r2 lifecycle --account ACCOUNT_ID --bucket my-bucket`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		res, err := c.Client.R2.Buckets.Lifecycle.Get(cmd.Context(), bucket,
			r2.BucketLifecycleGetParams{AccountID: cf.F(c.AccountID)})
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		rows := []LifecycleRow{}
		for _, r := range res.Rules {
			row := LifecycleRow{ID: r.ID, Enabled: r.Enabled, Prefix: r.Conditions.Prefix}
			if d := r.DeleteObjectsTransition.Condition; d.Type != "" {
				row.DeleteObjects = transition(string(d.Type), d.MaxAge, d.Date.IsZero(), d.Date.Format("2006-01-02"))
			}
			if a := r.AbortMultipartUploadsTransition.Condition; a.Type != "" {
				row.AbortUploads = transition(string(a.Type), a.MaxAge, true, "")
			}
			rows = append(rows, row)
		}
		if p.JSON || p.TOON {
			p.PrintResult(rows)
			return nil
		}
		if len(rows) == 0 {
			p.Info("No lifecycle rules on bucket %s.", bucket)
			return nil
		}
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			table = append(table, []string{r.ID, fmt.Sprint(r.Enabled), r.Prefix, r.DeleteObjects, r.AbortUploads})
		}
		p.Table([]string{"ID", "ENABLED", "PREFIX", "DELETE_OBJECTS", "ABORT_UPLOADS"}, table)
		return nil
	},
}

// transition renders a lifecycle condition as "after 7d" or "on 2027-01-01".
func transition(kind string, maxAgeSeconds int64, noDate bool, date string) string {
	if kind == "Date" && !noDate {
		return "on " + date
	}
	return fmt.Sprintf("after %dd", maxAgeSeconds/86400)
}
