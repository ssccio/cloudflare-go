package zones

import (
	"context"
	"fmt"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var lookupCmd = &cobra.Command{
	Use:   "lookup <domain>",
	Short: "Look up the Zone ID for a domain name",
	Long: `Look up the Cloudflare Zone ID for a given domain name.

Examples:
  cf zones lookup example.com
  cf zones lookup example.com --json`,
	Args: cobra.ExactArgs(1),
	RunE: runLookup,
}

func runLookup(cmd *cobra.Command, args []string) error {
	domain := args[0]

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

	p.Info("Looking up zone ID for %s…", domain)

	iter := cfClient.Zones.ListAutoPaging(context.Background(), zones.ZoneListParams{
		Name: cf.F(domain),
	})

	var found []zoneResult
	for iter.Next() {
		z := iter.Current()
		found = append(found, zoneResult{
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

	if len(found) == 0 {
		p.Error("no zone found for domain %q — check the domain name and that your token has access", domain)
		return fmt.Errorf("zone not found: %s", domain)
	}

	if p.JSON || p.TOON {
		if len(found) == 1 {
			p.PrintResult(found[0])
		} else {
			p.PrintResult(found)
		}
		return nil
	}

	for _, z := range found {
		p.KV([][2]string{
			{"Domain", z.Name},
			{"Zone ID", z.ID},
			{"Status", z.Status},
			{"Plan", z.Plan},
		})
	}
	return nil
}
