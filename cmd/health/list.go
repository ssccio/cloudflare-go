package health

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/healthchecks"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	listZone   string
	listDomain string
)

// healthcheckResult is the serialisable result for --json / --toon output.
//
// Note: the v6 SDK's Healthcheck type has no "enabled" field — it exposes
// "suspended" instead ("If suspended, no health checks are sent to the
// origin."). Enabled is derived here as !Suspended to match the field the
// spec asked for.
type healthcheckResult struct {
	ID          string `json:"id"          toon:"id"`
	Name        string `json:"name"        toon:"name"`
	Address     string `json:"address"     toon:"address"`
	Type        string `json:"type"        toon:"type"`
	Enabled     bool   `json:"enabled"     toon:"enabled"`
	Status      string `json:"status"      toon:"status"`
	Description string `json:"description" toon:"description"`
	Interval    int64  `json:"interval"    toon:"interval"`
	Timeout     int64  `json:"timeout"     toon:"timeout"`
	Retries     int64  `json:"retries"     toon:"retries"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List health checks in a zone",
	Long: `List all health checks configured on a Cloudflare zone.

Examples:
  cf health list --zone ZONE_ID
  cf health list --domain example.com
  cf health list --domain example.com --json
  cf health list --domain example.com --toon --query "[?status=='unhealthy'].name"`,
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

	p.Info("Fetching health checks for zone %s…", c.ZoneID)

	var checks []healthcheckResult
	iter := c.Client.Healthchecks.ListAutoPaging(cmd.Context(), healthchecks.HealthcheckListParams{
		ZoneID: cf.F(c.ZoneID),
	})
	for iter.Next() {
		h := iter.Current()
		checks = append(checks, healthcheckResult{
			ID:          h.ID,
			Name:        h.Name,
			Address:     h.Address,
			Type:        h.Type,
			Enabled:     !h.Suspended,
			Status:      string(h.Status),
			Description: h.Description,
			Interval:    h.Interval,
			Timeout:     h.Timeout,
			Retries:     h.Retries,
		})
	}
	if err := iter.Err(); err != nil {
		p.Error("API error: %v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(checks)
		return nil
	}

	if len(checks) == 0 {
		p.Info("No health checks configured.")
		return nil
	}

	rows := make([][]string, 0, len(checks))
	for _, h := range checks {
		enabled := "✓"
		if !h.Enabled {
			enabled = "—"
		}
		rows = append(rows, []string{
			h.Name,
			h.Address,
			h.Type,
			h.Status,
			enabled,
			h.ID,
		})
	}

	p.Table(
		[]string{"NAME", "ADDRESS", "TYPE", "STATUS", "ENABLED", "ID"},
		rows,
	)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d health check(s)\n", len(checks))
	return nil
}
