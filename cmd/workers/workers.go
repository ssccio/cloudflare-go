// Package workers implements the `cf workers` subcommand group.
package workers

import (
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/workers"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

// Cmd is the `cf workers` parent command.
var Cmd = &cobra.Command{
	Use:   "workers",
	Short: "Read Workers scripts",
	Long:  "List the account's Workers scripts.\n\nNeeds Account → Workers Scripts → Read on the token.",
}

var account string

func init() {
	Cmd.PersistentFlags().StringVar(&account, "account", "", "Account ID (or CLOUDFLARE_ACCOUNT_ID)")
	Cmd.AddCommand(listCmd)
}

type scriptRow struct {
	Name       string `json:"name"        toon:"name"`
	ModifiedOn string `json:"modified_on" toon:"modified_on"`
	CreatedOn  string `json:"created_on"  toon:"created_on"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List the account's Workers scripts",
	Long: `List the account's Workers scripts with their last deploy time.

Examples:
  cf workers list --account ACCOUNT_ID`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		c, err := cmdutil.Account(cmd, account)
		if err != nil {
			return err
		}
		p := c.Printer
		page, err := c.Client.Workers.Scripts.List(cmd.Context(), workers.ScriptListParams{AccountID: cf.F(c.AccountID)})
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		rows := []scriptRow{}
		for _, s := range page.Result {
			rows = append(rows, scriptRow{Name: s.ID, ModifiedOn: stamp(s.ModifiedOn), CreatedOn: stamp(s.CreatedOn)})
		}
		if p.JSON || p.TOON {
			p.PrintResult(rows)
			return nil
		}
		if len(rows) == 0 {
			p.Info("No Workers scripts visible. An empty list can also mean the token lacks Workers Scripts → Read.")
			return nil
		}
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			table = append(table, []string{r.Name, r.ModifiedOn, r.CreatedOn})
		}
		p.Table([]string{"NAME", "MODIFIED", "CREATED"}, table)
		return nil
	},
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}
