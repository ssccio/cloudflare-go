// Package logpush implements the `cf logpush` subcommand group: zone-scoped
// Logpush jobs and the destination ownership challenge they require.
package logpush

import (
	"fmt"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/logpush"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

// Cmd is the `cf logpush` parent command.
var Cmd = &cobra.Command{
	Use:   "logpush",
	Short: "Manage zone Logpush jobs",
	Long: `List, create and delete zone-scoped Logpush jobs. REST only.

Needs Zone → Logs → Edit on the token (Read is enough for list).

Creating a job to a new destination is two steps: "cf logpush ownership"
asks Cloudflare to write a challenge file to the destination, then
"cf logpush create --ownership-challenge <file contents>" creates the job.`,
}

func init() {
	Cmd.AddCommand(listCmd)
	Cmd.AddCommand(ownershipCmd)
	Cmd.AddCommand(createCmd)
	Cmd.AddCommand(deleteCmd)
}

var (
	listZone   string
	listDomain string
)

// JobRow is the summary of one Logpush job.
type JobRow struct {
	ID           int64  `json:"id"            toon:"id"`
	Name         string `json:"name"          toon:"name"`
	Dataset      string `json:"dataset"       toon:"dataset"`
	Enabled      bool   `json:"enabled"       toon:"enabled"`
	Destination  string `json:"destination"   toon:"destination"`
	LastComplete string `json:"last_complete" toon:"last_complete"`
	LastError    string `json:"last_error"    toon:"last_error"`
	ErrorMessage string `json:"error_message" toon:"error_message"`
}

func jobRow(j logpush.LogpushJob) JobRow {
	r := JobRow{ID: j.ID, Name: j.Name, Dataset: string(j.Dataset), Enabled: j.Enabled, Destination: j.DestinationConf, ErrorMessage: j.ErrorMessage}
	if !j.LastComplete.IsZero() {
		r.LastComplete = j.LastComplete.UTC().Format("2006-01-02T15:04:05Z")
	}
	if !j.LastError.IsZero() {
		r.LastError = j.LastError.UTC().Format("2006-01-02T15:04:05Z")
	}
	return r
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List a zone's Logpush jobs",
	Long: `List the zone's Logpush jobs with their dataset, destination and last
push times. A job whose last_error is newer than last_complete is failing.

Examples:
  cf logpush list --domain example.com
  cf logpush list --domain example.com --json`,
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVar(&listZone, "zone", "", "Zone ID")
	listCmd.Flags().StringVar(&listDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	listCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runList(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, listZone, listDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	page, err := c.Client.Logpush.Jobs.List(cmd.Context(), logpush.JobListParams{ZoneID: cf.F(c.ZoneID)})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	rows := []JobRow{}
	for _, j := range page.Result {
		rows = append(rows, jobRow(j))
	}

	if p.JSON || p.TOON {
		p.PrintResult(rows)
		return nil
	}
	if len(rows) == 0 {
		p.Info("No Logpush jobs on zone %s.", c.ZoneID)
		return nil
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{fmt.Sprint(r.ID), r.Name, r.Dataset, fmt.Sprint(r.Enabled), r.Destination, r.LastComplete, r.LastError})
	}
	p.Table([]string{"ID", "NAME", "DATASET", "ENABLED", "DESTINATION", "LAST_COMPLETE", "LAST_ERROR"}, table)
	return nil
}
