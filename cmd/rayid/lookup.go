package rayid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	lookupZone   string
	lookupDomain string
	lookupSince  string
	lookupUntil  string
)

// firewallEventRaw matches the Cloudflare GraphQL field names for deserialization only.
type firewallEventRaw struct {
	Action             string    `json:"action"`
	ClientAsn          string    `json:"clientAsn"`
	ClientCountryName  string    `json:"clientCountryName"`
	ClientIP           string    `json:"clientIP"`
	ClientRequestHost  string    `json:"clientRequestHTTPHost"`
	ClientRequestPath  string    `json:"clientRequestPath"`
	ClientRequestQuery string    `json:"clientRequestQuery"`
	Datetime           time.Time `json:"datetime"`
	RayName            string    `json:"rayName"`
	RuleID             string    `json:"ruleId"`
	Source             string    `json:"source"`
	UserAgent          string    `json:"userAgent"`
}

// FirewallEvent is the user-facing struct with clean, consistent field names.
// JSON tags match TOON tags so --json, --toon, and --query all use the same names.
type FirewallEvent struct {
	Action    string    `json:"action"   toon:"action"`
	ASN       string    `json:"asn"      toon:"asn"`
	Country   string    `json:"country"  toon:"country"`
	IP        string    `json:"ip"       toon:"ip"`
	Host      string    `json:"host"     toon:"host"`
	Path      string    `json:"path"     toon:"path"`
	Query     string    `json:"query"    toon:"query"`
	Datetime  time.Time `json:"datetime" toon:"datetime"`
	RayID     string    `json:"ray_id"   toon:"ray_id"`
	RuleID    string    `json:"rule_id"  toon:"rule_id"`
	Source    string    `json:"source"   toon:"source"`
	UserAgent string    `json:"ua"       toon:"ua"`
}

// HTTPRequest holds the result of an httpRequestsAdaptive lookup (non-WAF requests).
type HTTPRequest struct {
	Datetime     time.Time `json:"datetime"      toon:"datetime"`
	RayID        string    `json:"ray_id"        toon:"ray_id"`
	IP           string    `json:"ip"            toon:"ip"`
	Host         string    `json:"host"          toon:"host"`
	Path         string    `json:"path"          toon:"path"`
	EdgeStatus   int       `json:"edge_status"   toon:"edge_status"`
	OriginStatus int       `json:"origin_status" toon:"origin_status"`
	CacheStatus  string    `json:"cache_status"  toon:"cache_status"`
	UserAgent    string    `json:"ua"            toon:"ua"`
}

// RayIDResult is the top-level result for --json / --toon output.
type RayIDResult struct {
	RayID        string          `json:"ray_id"         toon:"ray_id"`
	ZoneID       string          `json:"zone_id"        toon:"zone_id"`
	Since        string          `json:"since"          toon:"since"`
	Until        string          `json:"until"          toon:"until"`
	Events       []FirewallEvent `json:"events"         toon:"events"`
	HTTPRequests []HTTPRequest   `json:"http_requests"  toon:"http_requests"`
}

const graphqlEndpoint = "https://api.cloudflare.com/client/v4/graphql"

var lookupCmd = &cobra.Command{
	Use:   "lookup <ray-id>",
	Short: "Look up a Cloudflare Ray ID",
	Long: `Look up a Cloudflare Ray ID to retrieve the action taken,
firewall rule matched, client details, and request metadata.

Queries the Cloudflare GraphQL Security Analytics API.
Use --since / --until to narrow the time window (reduces rate limit usage).
Either --zone or --domain is required.

Examples:
  cf rayid lookup 7f9b3c1a4e5d6f8a --domain example.com
  cf rayid lookup 7f9b3c1a4e5d6f8a --zone ZONE_ID --since 2h
  cf rayid lookup 7f9b3c1a4e5d6f8a --zone ZONE_ID --since 24h --json
  cf rayid lookup 7f9b3c1a4e5d6f8a --domain example.com --json --query 'events[*].action'
  cf rayid lookup 7f9b3c1a4e5d6f8a --domain example.com --toon --query 'events[*].{ray: ray_id, action: action}'`,
	Args: cobra.ExactArgs(1),
	RunE: runLookup,
}

func init() {
	lookupCmd.Flags().StringVar(&lookupZone, "zone", "", "Zone ID to scope the search")
	lookupCmd.Flags().StringVar(&lookupDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	lookupCmd.Flags().StringVar(&lookupSince, "since", "1h", "How far back to search (e.g. 1h, 6h, 24h, 48h)")
	lookupCmd.Flags().StringVar(&lookupUntil, "until", "", "End of search window (RFC3339 or relative like 1h); defaults to now")
	lookupCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runLookup(cmd *cobra.Command, args []string) error {
	rayID := args[0]

	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	toonFlag, _ := cmd.Root().PersistentFlags().GetBool("toon")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")
	query, _ := cmd.Root().PersistentFlags().GetString("query")

	p := output.New(jsonFlag, toonFlag, quiet, noColor, query)

	if token == "" {
		token = os.Getenv("CLOUDFLARE_API_TOKEN")
	}
	if token == "" {
		err := fmt.Errorf("Cloudflare API token required: set CLOUDFLARE_API_TOKEN or use --token")
		p.Error("%v", err)
		return err
	}

	if lookupZone == "" && lookupDomain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return err
	}

	zoneID := lookupZone
	if lookupDomain != "" {
		cfClient, err := client.New(client.Config{Token: token})
		if err != nil {
			p.Error("%v", err)
			return err
		}
		p.Info("Resolving zone ID for %s…", lookupDomain)
		zoneID, err = client.ResolveZoneID(cmd.Context(), cfClient, "", lookupDomain)
		if err != nil {
			p.Error("%v", err)
			return err
		}
	}

	// Parse time window.
	until := time.Now().UTC()
	if lookupUntil != "" {
		d, err := parseDuration(lookupUntil)
		if err == nil {
			until = time.Now().UTC().Add(-d)
		} else {
			until, err = time.Parse(time.RFC3339, lookupUntil)
			if err != nil {
				p.Error("invalid --until value %q: use RFC3339 or a duration like 1h", lookupUntil)
				return err
			}
		}
	}
	sinceDur, err := parseDuration(lookupSince)
	if err != nil {
		p.Error("invalid --since value %q: use a duration like 1h, 6h, 24h", lookupSince)
		return err
	}
	since := until.Add(-sinceDur)

	p.Info("Searching %s window (%s → %s)…",
		lookupSince,
		since.Format("2006-01-02 15:04 UTC"),
		until.Format("2006-01-02 15:04 UTC"),
	)

	events, err := queryFirewallEvents(cmd.Context(), token, zoneID, rayID, since, until)
	if err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}

	// Fall back to HTTP request logs if no WAF event found.
	var httpReqs []HTTPRequest
	if len(events) == 0 {
		httpReqs, err = queryHTTPRequest(cmd.Context(), token, zoneID, rayID, since, until)
		if err != nil {
			p.Info("HTTP request lookup failed: %v", err)
		}
	}

	result := RayIDResult{
		RayID:        rayID,
		ZoneID:       zoneID,
		Since:        since.Format(time.RFC3339),
		Until:        until.Format(time.RFC3339),
		Events:       events,
		HTTPRequests: httpReqs,
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if len(events) == 0 && len(httpReqs) == 0 {
		p.Info("No events found for Ray ID %s in the %s window.", rayID, lookupSince)
		p.Info("Try a wider window with --since 6h or --since 24h.")
		return nil
	}

	for i, ev := range events {
		if len(events) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "\nFirewall Event %d of %d\n", i+1, len(events))
		}
		url := ev.Host + ev.Path
		if ev.Query != "" {
			url += "?" + ev.Query
		}
		p.KV([][2]string{
			{"Ray ID", ev.RayID},
			{"Datetime", ev.Datetime.Format(time.RFC3339)},
			{"Action", strings.ToUpper(ev.Action)},
			{"Source", ev.Source},
			{"Rule ID", ev.RuleID},
			{"Client IP", ev.IP},
			{"Country", ev.Country},
			{"ASN", ev.ASN},
			{"Host", ev.Host},
			{"Path", ev.Path},
			{"Query", ev.Query},
			{"User Agent", ev.UserAgent},
			{"URL", url},
		})
	}

	for i, r := range httpReqs {
		if len(httpReqs) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "\nHTTP Request %d of %d\n", i+1, len(httpReqs))
		}
		url := r.Host + r.Path
		p.KV([][2]string{
			{"Ray ID", r.RayID},
			{"Datetime", r.Datetime.Format(time.RFC3339)},
			{"Edge Status", fmt.Sprintf("%d", r.EdgeStatus)},
			{"Origin Status", fmt.Sprintf("%d", r.OriginStatus)},
			{"Cache", r.CacheStatus},
			{"Client IP", r.IP},
			{"Host", r.Host},
			{"Path", r.Path},
			{"User Agent", r.UserAgent},
			{"URL", url},
		})
	}

	return nil
}

// queryFirewallEvents queries the Cloudflare GraphQL Security Analytics API.
// Variables are parameterized to prevent GraphQL injection.
func queryFirewallEvents(ctx context.Context, token, zoneID, rayID string, since, until time.Time) ([]FirewallEvent, error) {
	const gql = `
query FirewallEventsByRayID($zoneTag: string! $rayName: string! $since: Time! $until: Time!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      firewallEventsAdaptive(
        filter: {rayName: $rayName datetime_geq: $since datetime_leq: $until}
        limit: 1
        orderBy: [datetime_DESC]
      ) {
        action clientAsn clientCountryName clientIP
        clientRequestHTTPHost clientRequestPath clientRequestQuery
        datetime rayName ruleId source userAgent
      }
    }
  }
}`

	var gqlResp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					FirewallEventsAdaptive []firewallEventRaw `json:"firewallEventsAdaptive"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct{ Message string `json:"message"` } `json:"errors"`
	}

	if err := doGraphQL(ctx, token, gql, map[string]string{
		"zoneTag": zoneID,
		"rayName": rayID,
		"since":   since.Format(time.RFC3339),
		"until":   until.Format(time.RFC3339),
	}, &gqlResp); err != nil {
		return nil, err
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}

	if len(gqlResp.Data.Viewer.Zones) == 0 {
		return nil, nil
	}

	raw := gqlResp.Data.Viewer.Zones[0].FirewallEventsAdaptive
	events := make([]FirewallEvent, len(raw))
	for i, r := range raw {
		events[i] = FirewallEvent{
			Action:    r.Action,
			ASN:       r.ClientAsn,
			Country:   r.ClientCountryName,
			IP:        r.ClientIP,
			Host:      r.ClientRequestHost,
			Path:      r.ClientRequestPath,
			Query:     r.ClientRequestQuery,
			Datetime:  r.Datetime,
			RayID:     r.RayName,
			RuleID:    r.RuleID,
			Source:    r.Source,
			UserAgent: r.UserAgent,
		}
	}
	return events, nil
}

// queryHTTPRequest falls back to httpRequestsAdaptive for non-WAF Ray IDs (e.g. 5xx origin errors).
func queryHTTPRequest(ctx context.Context, token, zoneID, rayID string, since, until time.Time) ([]HTTPRequest, error) {
	const gql = `
query HTTPRequestByRayID($zoneTag: string! $rayName: string! $since: Time! $until: Time!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      httpRequestsAdaptive(
        filter: {rayName: $rayName datetime_geq: $since datetime_leq: $until}
        limit: 1
        orderBy: [datetime_DESC]
      ) {
        datetime rayName clientIP
        clientRequestHTTPHost clientRequestPath
        edgeResponseStatus originResponseStatus
        cacheStatus clientRequestUserAgent
      }
    }
  }
}`

	var gqlResp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					HTTPRequestsAdaptive []struct {
						Datetime             time.Time `json:"datetime"`
						RayName              string    `json:"rayName"`
						ClientIP             string    `json:"clientIP"`
						ClientRequestHost    string    `json:"clientRequestHTTPHost"`
						ClientRequestPath    string    `json:"clientRequestPath"`
						EdgeResponseStatus   int       `json:"edgeResponseStatus"`
						OriginResponseStatus int       `json:"originResponseStatus"`
						CacheStatus          string    `json:"cacheStatus"`
						UserAgent            string    `json:"clientRequestUserAgent"`
					} `json:"httpRequestsAdaptive"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct{ Message string `json:"message"` } `json:"errors"`
	}

	if err := doGraphQL(ctx, token, gql, map[string]string{
		"zoneTag": zoneID,
		"rayName": rayID,
		"since":   since.Format(time.RFC3339),
		"until":   until.Format(time.RFC3339),
	}, &gqlResp); err != nil {
		return nil, err
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}

	if len(gqlResp.Data.Viewer.Zones) == 0 {
		return nil, nil
	}

	raw := gqlResp.Data.Viewer.Zones[0].HTTPRequestsAdaptive
	reqs := make([]HTTPRequest, len(raw))
	for i, r := range raw {
		reqs[i] = HTTPRequest{
			Datetime:     r.Datetime,
			RayID:        r.RayName,
			IP:           r.ClientIP,
			Host:         r.ClientRequestHost,
			Path:         r.ClientRequestPath,
			EdgeStatus:   r.EdgeResponseStatus,
			OriginStatus: r.OriginResponseStatus,
			CacheStatus:  r.CacheStatus,
			UserAgent:    r.UserAgent,
		}
	}
	return reqs, nil
}

// doGraphQL executes a parameterized GraphQL query against the Cloudflare API.
func doGraphQL(ctx context.Context, token, query string, variables map[string]string, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return fmt.Errorf("marshalling query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return json.Unmarshal(respBody, out)
}

// parseDuration parses a human duration string like "1h", "24h", "48h".
func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 1h, 6h, 24h)", s)
	}
	return d, nil
}
