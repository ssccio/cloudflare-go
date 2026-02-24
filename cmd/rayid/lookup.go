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

// FirewallEvent represents a single event from the GraphQL firewallEventsAdaptive dataset.
type FirewallEvent struct {
	Action             string    `json:"action"                toon:"action"`
	ClientAsn          string    `json:"clientAsn"             toon:"asn"`
	ClientCountryName  string    `json:"clientCountryName"     toon:"country"`
	ClientIP           string    `json:"clientIP"              toon:"ip"`
	ClientRequestHost  string    `json:"clientRequestHTTPHost" toon:"host"`
	ClientRequestPath  string    `json:"clientRequestPath"     toon:"path"`
	ClientRequestQuery string    `json:"clientRequestQuery"    toon:"query"`
	Datetime           time.Time `json:"datetime"              toon:"datetime"`
	RayName            string    `json:"rayName"               toon:"ray_id"`
	RuleID             string    `json:"ruleId"                toon:"rule_id"`
	Source             string    `json:"source"                toon:"source"`
	UserAgent          string    `json:"userAgent"             toon:"ua"`
}

// RayIDResult is the top-level result for --json / --toon output.
type RayIDResult struct {
	RayID  string          `json:"ray_id"  toon:"ray_id"`
	ZoneID string          `json:"zone_id" toon:"zone_id"`
	Since  string          `json:"since"   toon:"since"`
	Until  string          `json:"until"   toon:"until"`
	Events []FirewallEvent `json:"events"  toon:"events"`
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
	lookupCmd.Flags().StringVar(&lookupSince, "since", "24h", "How far back to search (e.g. 1h, 6h, 24h, 48h)")
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

	result := RayIDResult{
		RayID:  rayID,
		ZoneID: zoneID,
		Since:  since.Format(time.RFC3339),
		Until:  until.Format(time.RFC3339),
		Events: events,
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if len(events) == 0 {
		p.Info("No firewall events found for Ray ID %s in the %s window.", rayID, lookupSince)
		p.Info("Try a wider window with --since 48h or --since 72h.")
		return nil
	}

	for i, ev := range events {
		if len(events) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "\nEvent %d of %d\n", i+1, len(events))
		}
		url := ev.ClientRequestHost + ev.ClientRequestPath
		if ev.ClientRequestQuery != "" {
			url += "?" + ev.ClientRequestQuery
		}
		p.KV([][2]string{
			{"Ray ID", rayID},
			{"Datetime", ev.Datetime.Format(time.RFC3339)},
			{"Action", strings.ToUpper(ev.Action)},
			{"Source", ev.Source},
			{"Rule ID", ev.RuleID},
			{"Client IP", ev.ClientIP},
			{"Country", ev.ClientCountryName},
			{"ASN", ev.ClientAsn},
			{"Host", ev.ClientRequestHost},
			{"Path", ev.ClientRequestPath},
			{"Query", ev.ClientRequestQuery},
			{"User Agent", ev.UserAgent},
			{"URL", url},
		})
	}
	return nil
}

// queryFirewallEvents queries the Cloudflare GraphQL Security Analytics API.
// A time window is required to reduce quota usage and avoid rate limits.
// Variables are parameterized to prevent GraphQL injection.
func queryFirewallEvents(ctx context.Context, token, zoneID, rayID string, since, until time.Time) ([]FirewallEvent, error) {
	const query = `
query FirewallEventsByRayID(
  $zoneTag: string!
  $rayName: string!
  $since: Time!
  $until: Time!
) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      firewallEventsAdaptive(
        filter: {
          rayName: $rayName
          datetime_geq: $since
          datetime_leq: $until
        }
        limit: 10
        orderBy: [datetime_DESC]
      ) {
        action
        clientAsn
        clientCountryName
        clientIP
        clientRequestHTTPHost
        clientRequestPath
        clientRequestQuery
        datetime
        rayName
        ruleId
        source
        userAgent
      }
    }
  }
}`

	payload := map[string]any{
		"query": query,
		"variables": map[string]string{
			"zoneTag": zoneID,
			"rayName": rayID,
			"since":   since.Format(time.RFC3339),
			"until":   until.Format(time.RFC3339),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshalling query: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var gqlResp struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					FirewallEventsAdaptive []FirewallEvent `json:"firewallEventsAdaptive"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		msgs := make([]string, 0, len(gqlResp.Errors))
		for _, e := range gqlResp.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}

	if len(gqlResp.Data.Viewer.Zones) == 0 {
		return nil, nil
	}

	return gqlResp.Data.Viewer.Zones[0].FirewallEventsAdaptive, nil
}

// parseDuration parses a human duration string like "1h", "24h", "48h".
func parseDuration(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 1h, 6h, 24h)", s)
	}
	return d, nil
}
