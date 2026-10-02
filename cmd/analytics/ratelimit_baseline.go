package analytics

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/graphql"
)

var (
	rlBaselineZone     string
	rlBaselineDomain   string
	rlBaselineHours    int
	rlBaselineUntil    string
	rlBaselineHost     string
	rlBaselineExclude  []string
	rlBaselineTop      int
	rlBaselineByHost   bool
	rlBaselineUncached bool
)

// defaultBaselineExcludes mirrors what a dynamic-page rate limit leaves out:
// API and SCORM traffic, admin, and static assets.
var defaultBaselineExcludes = []string{
	"/api/%", "/scormpostback/%", "/admin/%",
	"%.css", "%.js", "%.map", "%.png", "%.jpg", "%.jpeg", "%.gif", "%.svg",
	"%.webp", "%.ico", "%.woff", "%.woff2", "%.ttf",
}

// rlBaselineGroupRaw matches the Cloudflare GraphQL field names for deserialization only.
type rlBaselineGroupRaw struct {
	Count      int64 `json:"count"`
	Dimensions struct {
		ClientAsn            string `json:"clientAsn"`
		ClientASNDescription string `json:"clientASNDescription"`
		ColoCode             string `json:"coloCode"`
		Host                 string `json:"clientRequestHTTPHost"`
		DatetimeMinute       string `json:"datetimeMinute"`
	} `json:"dimensions"`
}

// RLBaselineRow is the busiest minute seen for one ASN at one colo.
type RLBaselineRow struct {
	ASN         string `json:"asn"          toon:"asn"`
	Description string `json:"description"  toon:"description"`
	Colo        string `json:"colo"         toon:"colo"`
	Host        string `json:"host,omitempty" toon:"host,omitempty"`
	PeakMinute  string `json:"peak_minute"  toon:"peak_minute"`
	PeakCount   int64  `json:"peak_count"   toon:"peak_count"`
}

// RLBaselineResult is the top-level result for --json / --toon output.
type RLBaselineResult struct {
	ZoneID   string          `json:"zone_id"  toon:"zone_id"`
	Host     string          `json:"host"     toon:"host"`
	Since    string          `json:"since"    toon:"since"`
	Until    string          `json:"until"    toon:"until"`
	Excludes []string        `json:"excludes" toon:"excludes"`
	Rows     []RLBaselineRow `json:"rows"     toon:"rows"`
}

var rlBaselineCmd = &cobra.Command{
	Use:   "ratelimit-baseline",
	Short: "Peak requests per minute per ASN and colo, for sizing a rate limit",
	Long: `Report the busiest minute for each ASN at each Cloudflare colo over a window.

This is the counter a rate limit keyed on ip.geoip.asnum + cf.colo.id with a
60s period would see, so the peaks give the floor for --rl-requests. Run it
over a normal window, not during an incident.

Static assets, /api/, /scormpostback/ and /admin/ are excluded by default.
--exclude (repeatable, SQL LIKE pattern) replaces that list.

Queries httpRequestsAdaptiveGroups, which is sampled; counts are estimates.
Keep --hours small, the analytics API budget is shared.

Examples:
  cf analytics ratelimit-baseline --domain example.com --hours 6
  cf analytics ratelimit-baseline --domain example.com --host www.example.com \
    --until 2026-10-01T20:00:00Z --hours 12`,
	RunE: runRLBaseline,
}

func init() {
	rlBaselineCmd.Flags().StringVar(&rlBaselineZone, "zone", "", "Zone ID to scope the query")
	rlBaselineCmd.Flags().StringVar(&rlBaselineDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	rlBaselineCmd.Flags().IntVar(&rlBaselineHours, "hours", 6, "Window length in hours")
	rlBaselineCmd.Flags().StringVar(&rlBaselineUntil, "until", "", "Window end, RFC3339 (default now)")
	rlBaselineCmd.Flags().StringVar(&rlBaselineHost, "host", "", "Limit to one hostname")
	rlBaselineCmd.Flags().StringSliceVar(&rlBaselineExclude, "exclude", nil, "Path LIKE patterns to exclude (replaces the defaults)")
	rlBaselineCmd.Flags().IntVar(&rlBaselineTop, "top", 25, "Number of ASN/colo rows to show")
	rlBaselineCmd.Flags().BoolVar(&rlBaselineByHost, "by-host", false, "Also group by hostname (matches an http.host rate-limit characteristic)")
	rlBaselineCmd.Flags().BoolVar(&rlBaselineUncached, "uncached", false, "Count only requests not served from cache (matches a requests-to-origin rate limit)")
	rlBaselineCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runRLBaseline(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, rlBaselineZone, rlBaselineDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	until := time.Now().UTC()
	if rlBaselineUntil != "" {
		until, err = time.Parse(time.RFC3339, rlBaselineUntil)
		if err != nil {
			p.Error("invalid --until: %v", err)
			return err
		}
	}
	since := until.Add(-time.Duration(rlBaselineHours) * time.Hour)
	sinceS, untilS := since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339)

	excludes := rlBaselineExclude
	if len(excludes) == 0 {
		excludes = defaultBaselineExcludes
	}
	and := make([]map[string]any, 0, len(excludes))
	for _, e := range excludes {
		and = append(and, map[string]any{"clientRequestPath_notlike": e})
	}
	filter := map[string]any{
		"datetime_geq":  sinceS,
		"datetime_leq":  untilS,
		"requestSource": "eyeball",
		"AND":           and,
	}
	if rlBaselineHost != "" {
		filter["clientRequestHTTPHost"] = rlBaselineHost
	}
	if rlBaselineUncached {
		filter["cacheStatus_neq"] = "hit"
	}

	p.Info("Querying per-ASN/colo minute peaks for zone %s (%s → %s)…", ctx.ZoneID, sinceS, untilS)

	hostDim := ""
	if rlBaselineByHost {
		hostDim = " clientRequestHTTPHost"
	}
	gql := `
query RLBaseline($zoneTag: string! $filter: ZoneHttpRequestsAdaptiveGroupsFilter_InputObject!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      httpRequestsAdaptiveGroups(filter: $filter limit: 5000 orderBy: [count_DESC]) {
        count
        dimensions { clientAsn clientASNDescription coloCode datetimeMinute` + hostDim + ` }
      }
    }
  }
}`

	var resp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					Groups []rlBaselineGroupRaw `json:"httpRequestsAdaptiveGroups"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []graphql.Error `json:"errors"`
	}

	if err := graphql.Do(cmd.Context(), ctx.Token, gql, map[string]any{
		"zoneTag": ctx.ZoneID,
		"filter":  filter,
	}, &resp); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}
	if err := graphql.CheckErrors(resp.Errors); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}

	// Rows arrive sorted by count, so the first one per ASN+colo is its peak.
	seen := map[string]bool{}
	result := RLBaselineResult{ZoneID: ctx.ZoneID, Host: rlBaselineHost, Since: sinceS, Until: untilS, Excludes: excludes}
	if len(resp.Data.Viewer.Zones) > 0 {
		for _, g := range resp.Data.Viewer.Zones[0].Groups {
			key := g.Dimensions.ClientAsn + "/" + g.Dimensions.ColoCode + "/" + g.Dimensions.Host
			if seen[key] {
				continue
			}
			seen[key] = true
			result.Rows = append(result.Rows, RLBaselineRow{
				ASN:         g.Dimensions.ClientAsn,
				Description: g.Dimensions.ClientASNDescription,
				Colo:        g.Dimensions.ColoCode,
				Host:        g.Dimensions.Host,
				PeakMinute:  g.Dimensions.DatetimeMinute,
				PeakCount:   g.Count,
			})
		}
	}
	sort.SliceStable(result.Rows, func(i, j int) bool { return result.Rows[i].PeakCount > result.Rows[j].PeakCount })
	if len(result.Rows) > rlBaselineTop {
		result.Rows = result.Rows[:rlBaselineTop]
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if len(result.Rows) == 0 {
		p.Info("No matching requests in the window.")
		return nil
	}

	rows := make([][]string, 0, len(result.Rows))
	for _, r := range result.Rows {
		row := []string{r.ASN, r.Description, r.Colo}
		if rlBaselineByHost {
			row = append(row, r.Host)
		}
		rows = append(rows, append(row, r.PeakMinute, fmt.Sprintf("%d", r.PeakCount)))
	}
	headers := []string{"ASN", "NAME", "COLO"}
	if rlBaselineByHost {
		headers = append(headers, "HOST")
	}
	p.Table(append(headers, "PEAK MINUTE", "REQ/MIN"), rows)
	return nil
}
