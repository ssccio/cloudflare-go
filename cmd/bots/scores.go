package bots

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/graphql"
)

var (
	scoresZone   string
	scoresDomain string
	scoresHours  int
	scoresUntil  string
	scoresHost   string
	scoresPath   string
	scoresBy     []string
	scoresMax    int
	scoresTop    int
)

// groupDims maps a --by value to its httpRequestsAdaptiveGroups dimension.
var groupDims = map[string]string{
	"host": "clientRequestHTTPHost",
	"path": "clientRequestPath",
	"ja4":  "ja4",
	"ja3":  "ja3Hash",
	"asn":  "clientAsn",
	"ua":   "userAgent",
}

type scoresGroupRaw struct {
	Count      int64          `json:"count"`
	Dimensions map[string]any `json:"dimensions"`
}

// ScoreRow is one group: a bot score band plus whatever --by dimensions were asked for.
type ScoreRow struct {
	Band  string            `json:"band"             toon:"band"`
	Keys  map[string]string `json:"keys,omitempty"   toon:"keys,omitempty"`
	Count int64             `json:"count"            toon:"count"`
}

// ScoresResult is the top-level result for --json / --toon output.
type ScoresResult struct {
	ZoneID string           `json:"zone_id" toon:"zone_id"`
	Since  string           `json:"since"   toon:"since"`
	Until  string           `json:"until"   toon:"until"`
	Bands  map[string]int64 `json:"bands,omitempty" toon:"bands,omitempty"`
	Rows   []ScoreRow       `json:"rows"    toon:"rows"`
}

var scoresCmd = &cobra.Command{
	Use:   "scores",
	Short: "Break a zone's traffic down by bot score band",
	Long: `Count requests by Bot Management score band over a window, optionally
grouped by host, path, JA4, JA3, ASN or user agent.

Bands follow Cloudflare's definitions:
  automated  score 1
  likely     2-29   (likely automated)
  human      30-99  (likely human)
  none       score 0, not computed (static resources, verified bots, etc.)

--max-score keeps only requests at or below a score, e.g. 29 for what a
"score lt 30" rule would match. Read-only.

One GraphQL call per run, kept small because the analytics quota is shared
and runs out quickly. Without --by it groups by score only (at most 100
rows). With --by it fetches only the --top rows, so band totals are not
shown. Check entitlement with "cf bots status" (REST) first, not with this.
Counts come from sampled data and are estimates.

Examples:
  cf bots scores --domain example.com --hours 6
  cf bots scores --domain example.com --path /app/accounts/ --by path
  cf bots scores --domain example.com --host api.example.com --by ja4 --by ua`,
	RunE: runScores,
}

func init() {
	scoresCmd.Flags().StringVar(&scoresZone, "zone", "", "Zone ID")
	scoresCmd.Flags().StringVar(&scoresDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	scoresCmd.Flags().IntVar(&scoresHours, "hours", 1, "Window length in hours")
	scoresCmd.Flags().StringVar(&scoresUntil, "until", "", "Window end, RFC3339 (default now)")
	scoresCmd.Flags().StringVar(&scoresHost, "host", "", "Limit to one hostname")
	scoresCmd.Flags().StringVar(&scoresPath, "path", "", "Limit to paths starting with this prefix")
	scoresCmd.Flags().StringSliceVar(&scoresBy, "by", nil, "Also group by: host, path, ja4, ja3, asn, ua (repeatable)")
	scoresCmd.Flags().IntVar(&scoresMax, "max-score", 0, "Only requests with score 1..N (0 = all)")
	scoresCmd.Flags().IntVar(&scoresTop, "top", 10, "Number of grouped rows to show")
	scoresCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func band(score int) string {
	switch {
	case score <= 0:
		return "none"
	case score == 1:
		return "automated"
	case score < 30:
		return "likely"
	default:
		return "human"
	}
}

func runScores(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, scoresZone, scoresDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	dims := []string{"botScore"}
	for _, b := range scoresBy {
		d, ok := groupDims[b]
		if !ok {
			err := fmt.Errorf("invalid --by %q; valid: host, path, ja4, ja3, asn, ua", b)
			p.Error("%v", err)
			return err
		}
		dims = append(dims, d)
	}

	until := time.Now().UTC()
	if scoresUntil != "" {
		until, err = time.Parse(time.RFC3339, scoresUntil)
		if err != nil {
			p.Error("invalid --until: %v", err)
			return err
		}
	}
	since := until.Add(-time.Duration(scoresHours) * time.Hour)
	sinceS, untilS := since.UTC().Format(time.RFC3339), until.UTC().Format(time.RFC3339)

	filter := map[string]any{
		"datetime_geq":  sinceS,
		"datetime_leq":  untilS,
		"requestSource": "eyeball",
	}
	if scoresHost != "" {
		filter["clientRequestHTTPHost"] = scoresHost
	}
	if scoresPath != "" {
		filter["clientRequestPath_like"] = scoresPath + "%"
	}
	if scoresMax > 0 {
		filter["botScore_geq"] = 1
		filter["botScore_leq"] = scoresMax
	}

	limit := 100
	if len(scoresBy) > 0 {
		limit = scoresTop
	}

	p.Info("Querying bot scores for zone %s (%s → %s)…", ctx.ZoneID, sinceS, untilS)

	gql := `
query BotScores($zoneTag: string! $filter: ZoneHttpRequestsAdaptiveGroupsFilter_InputObject!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      httpRequestsAdaptiveGroups(filter: $filter limit: ` + fmt.Sprint(limit) + ` orderBy: [count_DESC]) {
        count
        dimensions { ` + strings.Join(dims, " ") + ` }
      }
    }
  }
}`

	var resp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					Groups []scoresGroupRaw `json:"httpRequestsAdaptiveGroups"`
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

	result := ScoresResult{ZoneID: ctx.ZoneID, Since: sinceS, Until: untilS, Bands: map[string]int64{}}
	merged := map[string]*ScoreRow{}
	var order []string
	if len(resp.Data.Viewer.Zones) > 0 {
		for _, g := range resp.Data.Viewer.Zones[0].Groups {
			score := 0
			if f, ok := g.Dimensions["botScore"].(float64); ok {
				score = int(f)
			}
			b := band(score)
			result.Bands[b] += g.Count

			keys := map[string]string{}
			id := b
			for _, by := range scoresBy {
				v := fmt.Sprint(g.Dimensions[groupDims[by]])
				keys[by] = v
				id += "|" + v
			}
			if r, ok := merged[id]; ok {
				r.Count += g.Count
				continue
			}
			merged[id] = &ScoreRow{Band: b, Keys: keys, Count: g.Count}
			order = append(order, id)
		}
	}
	if len(scoresBy) > 0 {
		result.Bands = nil // partial when only --top rows were fetched
	}
	for _, id := range order {
		result.Rows = append(result.Rows, *merged[id])
	}
	sort.SliceStable(result.Rows, func(i, j int) bool { return result.Rows[i].Count > result.Rows[j].Count })
	if len(result.Rows) > scoresTop {
		result.Rows = result.Rows[:scoresTop]
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if len(result.Rows) == 0 {
		p.Info("No matching requests in the window.")
		return nil
	}

	var total int64
	for _, n := range result.Bands {
		total += n
	}
	bandRows := [][]string{}
	for _, b := range []string{"automated", "likely", "human", "none"} {
		n := result.Bands[b]
		bandRows = append(bandRows, []string{b, fmt.Sprintf("%d", n), fmt.Sprintf("%.1f%%", 100*float64(n)/float64(max(total, 1)))})
	}
	if len(scoresBy) == 0 {
		p.Table([]string{"BAND", "REQUESTS", "SHARE"}, bandRows)
		return nil
	}

	headers := []string{"BAND"}
	for _, by := range scoresBy {
		headers = append(headers, strings.ToUpper(by))
	}
	rows := make([][]string, 0, len(result.Rows))
	for _, r := range result.Rows {
		row := []string{r.Band}
		for _, by := range scoresBy {
			row = append(row, r.Keys[by])
		}
		rows = append(rows, append(row, fmt.Sprintf("%d", r.Count)))
	}
	p.Table(append(headers, "REQUESTS"), rows)
	return nil
}
