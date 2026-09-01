package analytics

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/graphql"
)

var (
	trafficZone   string
	trafficDomain string
	trafficHours  int
)

// httpRequests1hGroupRaw matches the Cloudflare GraphQL field names for deserialization only.
type httpRequests1hGroupRaw struct {
	Dimensions struct {
		Datetime string `json:"datetime"`
	} `json:"dimensions"`
	Sum struct {
		Requests       int64 `json:"requests"`
		Bytes          int64 `json:"bytes"`
		CachedRequests int64 `json:"cachedRequests"`
		CachedBytes    int64 `json:"cachedBytes"`
		Threats        int64 `json:"threats"`
		PageViews      int64 `json:"pageViews"`
	} `json:"sum"`
}

// TrafficHour is one hourly bucket of traffic totals.
type TrafficHour struct {
	Datetime       string `json:"datetime"        toon:"datetime"`
	Requests       int64  `json:"requests"        toon:"requests"`
	Bytes          int64  `json:"bytes"           toon:"bytes"`
	CachedRequests int64  `json:"cached_requests" toon:"cached_requests"`
	CachedBytes    int64  `json:"cached_bytes"    toon:"cached_bytes"`
	Threats        int64  `json:"threats"         toon:"threats"`
	PageViews      int64  `json:"page_views"      toon:"page_views"`
}

// TrafficResult is the top-level result for --json / --toon output.
type TrafficResult struct {
	ZoneID         string        `json:"zone_id"         toon:"zone_id"`
	Since          string        `json:"since"           toon:"since"`
	Until          string        `json:"until"           toon:"until"`
	TotalRequests  int64         `json:"total_requests"  toon:"total_requests"`
	TotalBytes     int64         `json:"total_bytes"     toon:"total_bytes"`
	CachedRequests int64         `json:"cached_requests" toon:"cached_requests"`
	TotalThreats   int64         `json:"total_threats"   toon:"total_threats"`
	Hours          []TrafficHour `json:"hours"           toon:"hours"`
}

var trafficCmd = &cobra.Command{
	Use:   "traffic",
	Short: "Show request traffic and bandwidth for a zone",
	Long: `Show hourly request traffic, cache ratio, bandwidth, and threats for a zone.

Queries the Cloudflare GraphQL Analytics API (httpRequests1hGroups).
Either --zone or --domain is required.

Examples:
  cf analytics traffic --domain example.com
  cf analytics traffic --zone ZONE_ID --hours 6
  cf analytics traffic --domain example.com --json --query 'hours[*].requests'`,
	RunE: runTraffic,
}

func init() {
	trafficCmd.Flags().StringVar(&trafficZone, "zone", "", "Zone ID to scope the query")
	trafficCmd.Flags().StringVar(&trafficDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	trafficCmd.Flags().IntVar(&trafficHours, "hours", 24, "How many hours back to query")
	trafficCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runTraffic(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, trafficZone, trafficDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	since, until := graphql.Window(trafficHours)
	p.Info("Querying traffic for zone %s (%s → %s)…", ctx.ZoneID, since, until)

	const gql = `
query Traffic($zoneTag: string! $since: Time! $until: Time!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      httpRequests1hGroups(
        filter: {datetime_geq: $since datetime_leq: $until}
        limit: 100
        orderBy: [datetime_ASC]
      ) {
        dimensions { datetime }
        sum { requests bytes cachedRequests cachedBytes threats pageViews }
      }
    }
  }
}`

	var resp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					HTTPRequests1hGroups []httpRequests1hGroupRaw `json:"httpRequests1hGroups"`
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

	result := TrafficResult{
		ZoneID: ctx.ZoneID,
		Since:  since,
		Until:  until,
	}

	if len(resp.Data.Viewer.Zones) > 0 {
		for _, g := range resp.Data.Viewer.Zones[0].HTTPRequests1hGroups {
			result.Hours = append(result.Hours, TrafficHour{
				Datetime:       g.Dimensions.Datetime,
				Requests:       g.Sum.Requests,
				Bytes:          g.Sum.Bytes,
				CachedRequests: g.Sum.CachedRequests,
				CachedBytes:    g.Sum.CachedBytes,
				Threats:        g.Sum.Threats,
				PageViews:      g.Sum.PageViews,
			})
			result.TotalRequests += g.Sum.Requests
			result.TotalBytes += g.Sum.Bytes
			result.CachedRequests += g.Sum.CachedRequests
			result.TotalThreats += g.Sum.Threats
		}
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if len(result.Hours) == 0 {
		p.Info("No traffic data found for the requested window.")
		return nil
	}

	cachedPct := 0.0
	if result.TotalRequests > 0 {
		cachedPct = float64(result.CachedRequests) / float64(result.TotalRequests) * 100
	}

	p.KV([][2]string{
		{"Zone ID", result.ZoneID},
		{"Window", fmt.Sprintf("%s → %s", result.Since, result.Until)},
		{"Requests", fmt.Sprintf("%d", result.TotalRequests)},
		{"Cached", fmt.Sprintf("%d (%.1f%%)", result.CachedRequests, cachedPct)},
		{"Bandwidth", fmt.Sprintf("%.2f MB", float64(result.TotalBytes)/1024/1024)},
		{"Threats", fmt.Sprintf("%d", result.TotalThreats)},
	})

	rows := make([][]string, 0, len(result.Hours))
	for _, h := range result.Hours {
		rows = append(rows, []string{
			h.Datetime,
			fmt.Sprintf("%d", h.Requests),
			fmt.Sprintf("%d", h.Threats),
		})
	}
	p.Table([]string{"TIME", "REQUESTS", "THREATS"}, rows)

	return nil
}
