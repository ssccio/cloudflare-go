package analytics

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/graphql"
)

var (
	wafEventsZone   string
	wafEventsDomain string
	wafEventsHours  int
	wafEventsLimit  int
	wafEventsHost   string
	wafEventsAction string
)

// firewallEventAdaptiveRaw matches the Cloudflare GraphQL field names for deserialization only.
type firewallEventAdaptiveRaw struct {
	Action                      string    `json:"action"`
	ClientASNDescription        string    `json:"clientASNDescription"`
	ClientCountryName           string    `json:"clientCountryName"`
	ClientIP                    string    `json:"clientIP"`
	ClientRequestHTTPHost       string    `json:"clientRequestHTTPHost"`
	ClientRequestHTTPMethodName string    `json:"clientRequestHTTPMethodName"`
	ClientRequestPath           string    `json:"clientRequestPath"`
	Datetime                    time.Time `json:"datetime"`
	RayName                     string    `json:"rayName"`
	RuleID                      string    `json:"ruleId"`
	RulesetID                   string    `json:"rulesetId"`
	Source                      string    `json:"source"`
	UserAgent                   string    `json:"userAgent"`
}

// WAFEvent is the user-facing struct with clean, consistent field names.
type WAFEvent struct {
	Action    string    `json:"action"     toon:"action"`
	ASN       string    `json:"asn"        toon:"asn"`
	Country   string    `json:"country"    toon:"country"`
	IP        string    `json:"ip"         toon:"ip"`
	Host      string    `json:"host"       toon:"host"`
	Method    string    `json:"method"     toon:"method"`
	Path      string    `json:"path"       toon:"path"`
	Datetime  time.Time `json:"datetime"   toon:"datetime"`
	RayID     string    `json:"ray_id"     toon:"ray_id"`
	RuleID    string    `json:"rule_id"    toon:"rule_id"`
	RulesetID string    `json:"ruleset_id" toon:"ruleset_id"`
	Source    string    `json:"source"     toon:"source"`
	UserAgent string    `json:"ua"         toon:"ua"`
}

// WAFEventsResult is the top-level result for --json / --toon output.
type WAFEventsResult struct {
	ZoneID string     `json:"zone_id" toon:"zone_id"`
	Since  string     `json:"since"   toon:"since"`
	Until  string     `json:"until"   toon:"until"`
	Events []WAFEvent `json:"events"  toon:"events"`
}

var wafEventsCmd = &cobra.Command{
	Use:   "waf-events",
	Short: "List recent WAF (firewall) events for a zone",
	Long: `List recent firewall events matched by WAF rules, custom rules, or rate limiting.

Queries the Cloudflare GraphQL Analytics API (firewallEventsAdaptive).
Use --host or --action to narrow the query server-side — this reduces the
chance of hitting the analytics API's rate limiter budget.
Either --zone or --domain is required.

Examples:
  cf analytics waf-events --domain example.com
  cf analytics waf-events --zone ZONE_ID --hours 6 --action block
  cf analytics waf-events --domain example.com --host www.example.com --limit 20
  cf analytics waf-events --domain example.com --json --query 'events[*].ip'`,
	RunE: runWAFEvents,
}

func init() {
	wafEventsCmd.Flags().StringVar(&wafEventsZone, "zone", "", "Zone ID to scope the query")
	wafEventsCmd.Flags().StringVar(&wafEventsDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	wafEventsCmd.Flags().IntVar(&wafEventsHours, "hours", 1, "How many hours back to query")
	wafEventsCmd.Flags().IntVar(&wafEventsLimit, "limit", 50, "Maximum number of events to return")
	wafEventsCmd.Flags().StringVar(&wafEventsHost, "host", "", "Filter by client request HTTP host")
	wafEventsCmd.Flags().StringVar(&wafEventsAction, "action", "", "Filter by firewall action (e.g. block, challenge, log)")
	wafEventsCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runWAFEvents(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, wafEventsZone, wafEventsDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	since, until := graphql.Window(wafEventsHours)
	p.Info("Querying WAF events for zone %s (%s → %s)…", ctx.ZoneID, since, until)

	const gql = `
query WAFEvents($zoneTag: string! $since: Time! $until: Time! $host: string $action: string $limit: int!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      firewallEventsAdaptive(
        filter: {datetime_geq: $since datetime_leq: $until clientRequestHTTPHost: $host action: $action}
        limit: $limit
        orderBy: [datetime_DESC]
      ) {
        action clientASNDescription clientCountryName clientIP
        clientRequestHTTPHost clientRequestHTTPMethodName clientRequestPath
        datetime rayName ruleId rulesetId source userAgent
      }
    }
  }
}`

	var resp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					FirewallEventsAdaptive []firewallEventAdaptiveRaw `json:"firewallEventsAdaptive"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []graphql.Error `json:"errors"`
	}

	variables := map[string]any{
		"zoneTag": ctx.ZoneID,
		"since":   since,
		"until":   until,
		"limit":   wafEventsLimit,
	}
	if wafEventsHost != "" {
		variables["host"] = wafEventsHost
	}
	if wafEventsAction != "" {
		variables["action"] = wafEventsAction
	}

	if err := graphql.Do(cmd.Context(), ctx.Token, gql, variables, &resp); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}
	if err := graphql.CheckErrors(resp.Errors); err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}

	result := WAFEventsResult{
		ZoneID: ctx.ZoneID,
		Since:  since,
		Until:  until,
	}

	if len(resp.Data.Viewer.Zones) > 0 {
		for _, r := range resp.Data.Viewer.Zones[0].FirewallEventsAdaptive {
			result.Events = append(result.Events, WAFEvent{
				Action:    r.Action,
				ASN:       r.ClientASNDescription,
				Country:   r.ClientCountryName,
				IP:        r.ClientIP,
				Host:      r.ClientRequestHTTPHost,
				Method:    r.ClientRequestHTTPMethodName,
				Path:      r.ClientRequestPath,
				Datetime:  r.Datetime,
				RayID:     r.RayName,
				RuleID:    r.RuleID,
				RulesetID: r.RulesetID,
				Source:    r.Source,
				UserAgent: r.UserAgent,
			})
		}
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if len(result.Events) == 0 {
		p.Info("No WAF events found for the requested window.")
		return nil
	}

	rows := make([][]string, 0, len(result.Events))
	for _, e := range result.Events {
		rows = append(rows, []string{
			e.Datetime.Format(time.RFC3339),
			e.Action,
			e.IP,
			e.Country,
			e.Host,
			e.Path,
		})
	}
	p.Table([]string{"TIME", "ACTION", "IP", "COUNTRY", "HOST", "PATH"}, rows)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d event(s)\n", len(result.Events))

	return nil
}
