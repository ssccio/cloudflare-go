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
	ruleHitsZone   string
	ruleHitsDomain string
	ruleHitsRules  []string
	ruleHitsDays   int
)

// RuleHitsRow is one rule's event count for one day and action.
type RuleHitsRow struct {
	Date   string `json:"date"    toon:"date"`
	RuleID string `json:"rule_id" toon:"rule_id"`
	Action string `json:"action"  toon:"action"`
	Count  int64  `json:"count"   toon:"count"`
}

var ruleHitsCmd = &cobra.Command{
	Use:   "rule-hits",
	Short: "Count firewall events per rule per day",
	Long: `Count firewall events for specific rule IDs, grouped by day and action.

One aggregated GraphQL call (firewallEventsAdaptiveGroups), sized to the
rules and days asked for. The analytics quota is shared and small, so pass
only the rules you need and keep --days short. Skip rules only produce
events when their "log matching requests" setting is on.

Examples:
  cf analytics rule-hits --domain example.com --rule-id RULE_ID --days 3
  cf analytics rule-hits --domain example.com --rule-id A --rule-id B --json`,
	RunE: runRuleHits,
}

func init() {
	ruleHitsCmd.Flags().StringVar(&ruleHitsZone, "zone", "", "Zone ID")
	ruleHitsCmd.Flags().StringVar(&ruleHitsDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	ruleHitsCmd.Flags().StringSliceVar(&ruleHitsRules, "rule-id", nil, "Rule ID to count (repeatable, required)")
	ruleHitsCmd.Flags().IntVar(&ruleHitsDays, "days", 3, "Days back to count, including today (UTC)")
	ruleHitsCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = ruleHitsCmd.MarkFlagRequired("rule-id")
}

func runRuleHits(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, ruleHitsZone, ruleHitsDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	now := time.Now().UTC()
	since := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(ruleHitsDays - 1))
	sinceS, untilS := since.Format(time.RFC3339), now.Format(time.RFC3339)

	// Upper bound on groups: rules x days x a few actions.
	limit := len(ruleHitsRules) * ruleHitsDays * 4

	p.Info("Counting events for %d rule(s) on zone %s (%s → %s)…", len(ruleHitsRules), ctx.ZoneID, sinceS, untilS)

	gql := `
query RuleHits($zoneTag: string! $filter: ZoneFirewallEventsAdaptiveGroupsFilter_InputObject!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      firewallEventsAdaptiveGroups(filter: $filter limit: ` + fmt.Sprint(limit) + `) {
        count
        dimensions { date ruleId action }
      }
    }
  }
}`

	var resp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					Groups []struct {
						Count      int64 `json:"count"`
						Dimensions struct {
							Date   string `json:"date"`
							RuleID string `json:"ruleId"`
							Action string `json:"action"`
						} `json:"dimensions"`
					} `json:"firewallEventsAdaptiveGroups"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []graphql.Error `json:"errors"`
	}
	if err := graphql.Do(cmd.Context(), ctx.Token, gql, map[string]any{
		"zoneTag": ctx.ZoneID,
		"filter": map[string]any{
			"datetime_geq": sinceS,
			"datetime_leq": untilS,
			"ruleId_in":    ruleHitsRules,
		},
	}, &resp); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}
	if err := graphql.CheckErrors(resp.Errors); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}

	rows := []RuleHitsRow{}
	if len(resp.Data.Viewer.Zones) > 0 {
		for _, g := range resp.Data.Viewer.Zones[0].Groups {
			rows = append(rows, RuleHitsRow{Date: g.Dimensions.Date, RuleID: g.Dimensions.RuleID, Action: g.Dimensions.Action, Count: g.Count})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RuleID != rows[j].RuleID {
			return rows[i].RuleID < rows[j].RuleID
		}
		return rows[i].Date < rows[j].Date
	})

	if p.JSON || p.TOON {
		p.PrintResult(rows)
		return nil
	}
	if len(rows) == 0 {
		p.Info("No events for those rules in the window.")
		return nil
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.RuleID, r.Date, r.Action, fmt.Sprintf("%d", r.Count)})
	}
	p.Table([]string{"RULE", "DATE", "ACTION", "EVENTS"}, table)
	return nil
}
