package rayid

import (
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

// LogEntry holds the fields we request from the Logpull API.
// Field names must match Cloudflare Logpull schema exactly.
type LogEntry struct {
	RayID                  string   `json:"RayID"`
	EdgeStartTimestamp     string   `json:"EdgeStartTimestamp"`
	ClientIP               string   `json:"ClientIP"`
	ClientCountry          string   `json:"ClientCountry"`
	ClientASN              int64    `json:"ClientASN"`
	ClientRequestHost      string   `json:"ClientRequestHost"`
	ClientRequestMethod    string   `json:"ClientRequestMethod"`
	ClientRequestPath      string   `json:"ClientRequestPath"`
	ClientRequestURI       string   `json:"ClientRequestURI"`
	ClientRequestUserAgent string   `json:"ClientRequestUserAgent"`
	ClientRequestProtocol  string   `json:"ClientRequestProtocol"`
	EdgeResponseStatus     int      `json:"EdgeResponseStatus"`
	WAFAction              string   `json:"WAFAction"`
	WAFRuleID              string   `json:"WAFRuleID"`
	WAFRuleMessage         string   `json:"WAFRuleMessage"`
	FirewallMatchesActions []string `json:"FirewallMatchesActions"`
	FirewallMatchesSources []string `json:"FirewallMatchesSources"`
	FirewallMatchesRuleIDs []string `json:"FirewallMatchesRuleIDs"`
	SecurityLevel          string   `json:"SecurityLevel"`
}

// RayIDResult is the top-level result for --json output.
type RayIDResult struct {
	RayID   string     `json:"ray_id"`
	ZoneID  string     `json:"zone_id"`
	Entries []LogEntry `json:"entries"`
}

// logpullFields is the comma-separated list of fields to request.
// Must be literal commas — the SDK URL-encodes them, so we use direct HTTP.
const logpullFields = "RayID,EdgeStartTimestamp,ClientIP,ClientCountry,ClientASN," +
	"ClientRequestHost,ClientRequestMethod,ClientRequestPath,ClientRequestURI," +
	"ClientRequestProtocol,ClientRequestUserAgent," +
	"EdgeResponseStatus,WAFAction,WAFRuleID,WAFRuleMessage," +
	"FirewallMatchesActions,FirewallMatchesSources,FirewallMatchesRuleIDs," +
	"SecurityLevel"

const logpullBase = "https://api.cloudflare.com/client/v4"

var lookupCmd = &cobra.Command{
	Use:   "lookup <ray-id>",
	Short: "Look up a Cloudflare Ray ID",
	Long: `Look up a Cloudflare Ray ID to retrieve the action taken,
firewall rule matched, client details, and full request metadata.

The Ray ID is visible in the CF-Ray response header on any request
proxied through Cloudflare.

Uses the Cloudflare Logpull REST API (GET /zones/{zone_id}/logs/rayids/{id}).
Either --zone or --domain is required to scope the query.

Note: Logpull requires the Logs: Read permission on your API token.

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

func runLookup(cmd *cobra.Command, args []string) error {
	rayID := args[0]

	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")

	p := output.New(jsonFlag, quiet, noColor)

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

	p.Info("Looking up Ray ID %s…", rayID)

	entries, err := fetchRayID(cmd.Context(), token, zoneID, rayID)
	if err != nil {
		p.Error("Logpull API error: %v", err)
		return err
	}

	result := RayIDResult{
		RayID:   rayID,
		ZoneID:  zoneID,
		Entries: entries,
	}

	if jsonFlag {
		p.PrintJSON(result)
		return nil
	}

	if len(entries) == 0 {
		p.Info("No log entries found for Ray ID %s.", rayID)
		p.Info("The ray ID may be outside the retention window, or your token may need the 'Logs: Read' permission.")
		return nil
	}

	for i, e := range entries {
		if len(entries) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "\nEntry %d of %d\n", i+1, len(entries))
		}
		p.KV([][2]string{
			{"Ray ID", e.RayID},
			{"Timestamp", e.EdgeStartTimestamp},
			{"Status", fmt.Sprintf("%d", e.EdgeResponseStatus)},
			{"Security Level", e.SecurityLevel},
			{"WAF Action", e.WAFAction},
			{"WAF Rule ID", e.WAFRuleID},
			{"WAF Rule", e.WAFRuleMessage},
			{"FW Actions", strings.Join(e.FirewallMatchesActions, ", ")},
			{"FW Sources", strings.Join(e.FirewallMatchesSources, ", ")},
			{"FW Rule IDs", strings.Join(e.FirewallMatchesRuleIDs, ", ")},
			{"Client IP", e.ClientIP},
			{"Country", e.ClientCountry},
			{"ASN", fmt.Sprintf("%d", e.ClientASN)},
			{"Method", e.ClientRequestMethod},
			{"Protocol", e.ClientRequestProtocol},
			{"Host", e.ClientRequestHost},
			{"URI", e.ClientRequestURI},
			{"User Agent", e.ClientRequestUserAgent},
		})
	}
	return nil
}

// fetchRayID queries the Cloudflare Logpull REST API directly using net/http.
// We bypass the SDK here because the SDK URL-encodes commas in query params,
// but the Logpull API requires literal comma-separated field names.
func fetchRayID(ctx context.Context, token, zoneID, rayID string) ([]LogEntry, error) {
	// Build URL with literal commas (not %2C) in the fields parameter.
	url := fmt.Sprintf("%s/zones/%s/logs/rayids/%s?fields=%s&timestamps=rfc3339",
		logpullBase, zoneID, rayID, logpullFields)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to extract a useful error message from the response.
		var errResp struct {
			Errors []struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"errors"`
		}
		if json.Unmarshal(body, &errResp) == nil && len(errResp.Errors) > 0 {
			msgs := make([]string, 0, len(errResp.Errors))
			for _, e := range errResp.Errors {
				msgs = append(msgs, e.Message)
			}
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.Join(msgs, "; "))
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return parseNDJSON(body)
}

// parseNDJSON parses a response that may be an array, single object, or NDJSON lines.
func parseNDJSON(b []byte) ([]LogEntry, error) {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	// JSON array.
	if strings.HasPrefix(trimmed, "[") {
		var entries []LogEntry
		if err := json.Unmarshal(b, &entries); err != nil {
			return nil, fmt.Errorf("parsing array response: %w", err)
		}
		return entries, nil
	}

	// Single object or NDJSON (one object per line).
	var entries []LogEntry
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e LogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parsing log line: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

