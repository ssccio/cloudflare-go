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

	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	lookupZone string
)

// FirewallEvent represents a single firewall event returned by the GraphQL API.
type FirewallEvent struct {
	Action                       string    `json:"action"`
	ClientASN                    int       `json:"clientASN"`
	ClientCountryName            string    `json:"clientCountryName"`
	ClientIP                     string    `json:"clientIP"`
	ClientRequestHTTPHost        string    `json:"clientRequestHTTPHost"`
	ClientRequestHTTPMethodName  string    `json:"clientRequestHTTPMethodName"`
	ClientRequestHTTPProtocol    string    `json:"clientRequestHTTPProtocol"`
	ClientRequestPath            string    `json:"clientRequestPath"`
	ClientRequestQuery           string    `json:"clientRequestQuery"`
	Datetime                     time.Time `json:"datetime"`
	RayName                      string    `json:"rayName"`
	RuleID                       string    `json:"ruleId"`
	Source                       string    `json:"source"`
	UserAgent                    string    `json:"userAgent"`
}

// RayIDResult is the top-level result for --json output.
type RayIDResult struct {
	RayID  string         `json:"ray_id"`
	Zone   string         `json:"zone_id"`
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
A zone ID is required to scope the query.

Examples:
  cf rayid lookup 7f9b3c1a4e5d6f8a --zone ZONE_ID
  cf rayid lookup 7f9b3c1a4e5d6f8a --zone ZONE_ID --json`,
	Args: cobra.ExactArgs(1),
	RunE: runLookup,
}

func init() {
	lookupCmd.Flags().StringVar(&lookupZone, "zone", "", "Zone ID to scope the search (required)")
	_ = lookupCmd.MarkFlagRequired("zone")
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

	p.Info("Looking up Ray ID %s in zone %s…", rayID, lookupZone)

	events, err := queryFirewallEvents(cmd.Context(), token, lookupZone, rayID)
	if err != nil {
		p.Error("GraphQL query failed: %v", err)
		return err
	}

	result := RayIDResult{
		RayID:  rayID,
		Zone:   lookupZone,
		Events: events,
	}

	if jsonFlag {
		p.PrintJSON(result)
		return nil
	}

	if len(events) == 0 {
		p.Info("No firewall events found for Ray ID %s in zone %s.", rayID, lookupZone)
		p.Info("The Ray ID may be from a non-firewall request, or outside the retention window.")
		return nil
	}

	for i, ev := range events {
		if len(events) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "\nEvent %d of %d\n", i+1, len(events))
		}
		url := ev.ClientRequestHTTPHost + ev.ClientRequestPath
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
			{"ASN", fmt.Sprintf("%d", ev.ClientASN)},
			{"Method", ev.ClientRequestHTTPMethodName},
			{"Protocol", ev.ClientRequestHTTPProtocol},
			{"Host", ev.ClientRequestHTTPHost},
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
func queryFirewallEvents(ctx context.Context, token, zoneID, rayID string) ([]FirewallEvent, error) {
	query := `{
  viewer {
    zones(filter: {zoneTag: "` + zoneID + `"}) {
      firewallEventsAdaptive(
        filter: {rayName: "` + rayID + `"}
        limit: 10
        orderBy: [datetime_DESC]
      ) {
        action
        clientASN
        clientCountryName
        clientIP
        clientRequestHTTPHost
        clientRequestHTTPMethodName
        clientRequestHTTPProtocol
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

	payload := map[string]string{"query": query}
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
