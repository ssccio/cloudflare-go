package zones

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudflare/cloudflare-go/v6/zones"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/output"
)

type zoneResult struct {
	ID     string `json:"id"     toon:"id"`
	Name   string `json:"name"   toon:"name"`
	Status string `json:"status" toon:"status"`
	Plan   string `json:"plan"   toon:"plan"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all zones in your account",
	Long: `List all Cloudflare zones (domains) in your account with their IDs.

Examples:
  cf zones list
  cf zones list --json
  cf zones list --json --query '[].id'
  cf zones list --toon --query '[].{name: name, id: id}'`,
	RunE: runList,
}

func runList(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	toonFlag, _ := cmd.Root().PersistentFlags().GetBool("toon")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")
	query, _ := cmd.Root().PersistentFlags().GetString("query")

	p := output.New(jsonFlag, toonFlag, quiet, noColor, query)

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Fetching zones…")

	var results []zoneResult
	iter := cfClient.Zones.ListAutoPaging(context.Background(), zones.ZoneListParams{})
	for iter.Next() {
		z := iter.Current()
		results = append(results, zoneResult{
			ID:     z.ID,
			Name:   z.Name,
			Status: string(z.Status),
			Plan:   z.Plan.Name,
		})
	}
	if err := iter.Err(); err != nil {
		p.Error("API error: %v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(results)
		return nil
	}

	if len(results) == 0 {
		p.Info("No zones found.")
		return nil
	}

	rows := make([][]string, 0, len(results))
	for _, z := range results {
		rows = append(rows, []string{z.Name, z.ID, z.Status, z.Plan})
	}
	p.Table([]string{"DOMAIN", "ZONE ID", "STATUS", "PLAN"}, rows)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d zone(s)\n", len(results))
	return nil
}
