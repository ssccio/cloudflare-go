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
)

// FirewallEvent represents a single firewall event returned by the GraphQL API.
// Field names match the Cloudflare GraphQL Analytics schema exactly.
type FirewallEvent struct {
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

// RayIDResult is the top-level result for --json output.
type RayIDResult struct {
	RayID  string          `json:"ray_id"`
	Zone   string          `json:"zone_id"`
	Events []FirewallEvent `json:"events"`
}

var lookupCmd = &cobra.Command{
	Use:   "lookup <ray-id>",
	Short: "Look up a Cloudflare Ray ID",
	Long: `Look up a Cloudflare Ray ID to retrieve the action taken,
firewall rule matched, client details, and full request metadata.

The Ray ID is visible in the CF-Ray response header on any request
proxied through Cloudflare. It can also be found in Cloudflare
security event logs and the Firewall Analytics dashboard.

This command queries the Cloudflare GraphQL Analytics API.
Either --zone or --domain is required to scope the query.

Examples:
  cf rayid lookup 7f9b3c1a4e5d6f8a --zone ZONE_ID
  cf rayid lookup 7f9b3c1a4e5d6f8a --domain example.com
  cf rayid lookup 7f9b3c1a4e5d6f8a --domain example.com --json`,
	Args: cobra.ExactArgs(1),
	RunE: runLookup,
}

func init() {
	lookupCmd.Flags().StringVar(&lookupZone, "zone", "", "Zone ID to scope the search")
	lookupCmd.Flags().StringVar(&lookupDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	lookupCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

const graphqlEndpoint = "https://api.cloudflare.com/client/v4/graphql"

func runLookup(cmd *cobra.Command, args []string) error {
	rayID := args[0]

	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")

	p := output.New(jsonFlag, quiet, noColor)

	// Resolve token: flag → env var
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

	p.Info("Looking up Ray ID %s in zone %s…", rayID, zoneID)

	events, err := queryFirewallEvents(cmd.Context(), token, zoneID, rayID)
	if err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}

	result := RayIDResult{
		RayID:  rayID,
		Zone:   zoneID,
		Events: events,
	}

	if jsonFlag {
		p.PrintJSON(result)
		return nil
	}

	if len(events) == 0 {
		p.Info("No firewall events found for Ray ID %s.", rayID)
		p.Info("The Ray ID may be from a non-firewall request, or outside the retention window.")
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

// queryFirewallEvents calls the Cloudflare GraphQL API to look up firewall
// events by Ray ID within a specific zone.
// Variables are passed separately from the query string to prevent GraphQL injection.
func queryFirewallEvents(ctx context.Context, token, zoneID, rayID string) ([]FirewallEvent, error) {
	const query = `
query FirewallEventsByRayID($zoneTag: string!, $rayName: string!) {
  viewer {
    zones(filter: {zoneTag: $zoneTag}) {
      firewallEventsAdaptive(
        filter: {rayName: $rayName}
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
		return nil, fmt.Errorf("GraphQL API returned HTTP %d: %s", resp.StatusCode, string(respBody))
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
		return nil, fmt.Errorf("GraphQL errors: %s", strings.Join(msgs, "; "))
	}

	if len(gqlResp.Data.Viewer.Zones) == 0 {
		return nil, nil
	}

	return gqlResp.Data.Viewer.Zones[0].FirewallEventsAdaptive, nil
}
