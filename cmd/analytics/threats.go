package analytics

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/graphql"
)

var (
	threatsZone   string
	threatsDomain string
	threatsHours  int
)

// firewallEventsAdaptiveGroupRaw matches the Cloudflare GraphQL field names for deserialization only.
type firewallEventsAdaptiveGroupRaw struct {
	Count      int64 `json:"count"`
	Dimensions struct {
		Action            string `json:"action"`
		ClientCountryName string `json:"clientCountryName"`
		Source            string `json:"source"`
	} `json:"dimensions"`
}

// ThreatBreakdown is one action/country/source aggregation bucket.
type ThreatBreakdown struct {
	Action  string `json:"action"  toon:"action"`
	Country string `json:"country" toon:"country"`
	Source  string `json:"source"  toon:"source"`
	Count   int64  `json:"count"   toon:"count"`
}

// ThreatsResult is the top-level result for --json / --toon output.
type ThreatsResult struct {
	ZoneID       string            `json:"zone_id"       toon:"zone_id"`
	Since        string            `json:"since"         toon:"since"`
	Until        string            `json:"until"         toon:"until"`
	TotalThreats int64             `json:"total_threats" toon:"total_threats"`
	Breakdown    []ThreatBreakdown `json:"breakdown"     toon:"breakdown"`
}

var threatsCmd = &cobra.Command{
	Use:   "threats",
	Short: "Show threat totals and firewall action breakdown for a zone",
	Long: `Show total threats and a breakdown of firewall actions by country and source.

Queries the Cloudflare GraphQL Analytics API (httpRequests1hGroups for the
total, firewallEventsAdaptiveGroups for the breakdown).
Either --zone or --domain is required.

Examples:
  cf analytics threats --domain example.com
  cf analytics threats --zone ZONE_ID --hours 6
  cf analytics threats --domain example.com --json --query 'breakdown[*].action'`,
	RunE: runThreats,
}

func init() {
	threatsCmd.Flags().StringVar(&threatsZone, "zone", "", "Zone ID to scope the query")
	threatsCmd.Flags().StringVar(&threatsDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	threatsCmd.Flags().IntVar(&threatsHours, "hours", 24, "How many hours back to query")
	threatsCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runThreats(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, threatsZone, threatsDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	since, until := graphql.Window(threatsHours)
	p.Info("Querying threats for zone %s (%s → %s)…", ctx.ZoneID, since, until)

	const gql = `
query Threats($zoneTag: string! $since: Time! $until: Time!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      httpRequests1hGroups(
        filter: {datetime_geq: $since datetime_leq: $until}
        limit: 100
      ) {
        sum { threats }
      }
      firewallEventsAdaptiveGroups(
        filter: {datetime_geq: $since datetime_leq: $until}
        limit: 20
        orderBy: [count_DESC]
      ) {
        count
        dimensions { action clientCountryName source }
      }
    }
  }
}`

	var resp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					HTTPRequests1hGroups []struct {
						Sum struct {
							Threats int64 `json:"threats"`
						} `json:"sum"`
					} `json:"httpRequests1hGroups"`
					FirewallEventsAdaptiveGroups []firewallEventsAdaptiveGroupRaw `json:"firewallEventsAdaptiveGroups"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []graphql.Error `json:"errors"`
	}

	if err := graphql.Do(cmd.Context(), ctx.Token, gql, map[string]any{
		"zoneTag": ctx.ZoneID,
		"since":   since,
		"until":   until,
	}, &resp); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}
	if err := graphql.CheckErrors(resp.Errors); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}

	result := ThreatsResult{
		ZoneID: ctx.ZoneID,
		Since:  since,
		Until:  until,
	}

	if len(resp.Data.Viewer.Zones) > 0 {
		zone := resp.Data.Viewer.Zones[0]
		for _, g := range zone.HTTPRequests1hGroups {
			result.TotalThreats += g.Sum.Threats
		}
		for _, g := range zone.FirewallEventsAdaptiveGroups {
			result.Breakdown = append(result.Breakdown, ThreatBreakdown{
				Action:  g.Dimensions.Action,
				Country: g.Dimensions.ClientCountryName,
				Source:  g.Dimensions.Source,
				Count:   g.Count,
			})
		}
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.KV([][2]string{
		{"Zone ID", result.ZoneID},
		{"Window", fmt.Sprintf("%s → %s", result.Since, result.Until)},
		{"Total Threats", fmt.Sprintf("%d", result.TotalThreats)},
	})

	if len(result.Breakdown) == 0 {
		p.Info("No firewall events found for the requested window.")
		return nil
	}

	rows := make([][]string, 0, len(result.Breakdown))
	for _, b := range result.Breakdown {
		rows = append(rows, []string{
			b.Action,
			b.Country,
			b.Source,
			fmt.Sprintf("%d", b.Count),
		})
	}
	p.Table([]string{"ACTION", "COUNTRY", "SOURCE", "COUNT"}, rows)

	return nil
}
