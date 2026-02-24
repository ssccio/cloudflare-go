package rayid

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	cflogs "github.com/cloudflare/cloudflare-go/v6/logs"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	lookupZone   string
	lookupDomain string
)

// LogEntry holds the fields we request from the Logpull API.
type LogEntry struct {
	RayID                   string   `json:"RayID"`
	EdgeStartTimestamp      string   `json:"EdgeStartTimestamp"`
	ClientIP                string   `json:"ClientIP"`
	ClientCountry           string   `json:"ClientCountry"`
	ClientASN               int64    `json:"ClientASN"`
	ClientRequestHost       string   `json:"ClientRequestHost"`
	ClientRequestMethod     string   `json:"ClientRequestMethod"`
	ClientRequestPath       string   `json:"ClientRequestPath"`
	ClientRequestQuery      string   `json:"ClientRequestQuery"`
	ClientRequestProtocol   string   `json:"ClientRequestProtocol"`
	WAFAction               string   `json:"WAFAction"`
	WAFRuleID               string   `json:"WAFRuleID"`
	FirewallMatchesActions  []string `json:"FirewallMatchesActions"`
	FirewallMatchesSources  []string `json:"FirewallMatchesSources"`
	FirewallMatchesRuleIDs  []string `json:"FirewallMatchesRuleIDs"`
	EdgeResponseStatus      int      `json:"EdgeResponseStatus"`
	UserAgent               string   `json:"UserAgent"`
}

// RayIDResult is the top-level result for --json output.
type RayIDResult struct {
	RayID   string     `json:"ray_id"`
	ZoneID  string     `json:"zone_id"`
	Entries []LogEntry `json:"entries"`
}

// logpullFields is the comma-separated list of fields to request from Logpull.
const logpullFields = "RayID,EdgeStartTimestamp,ClientIP,ClientCountry,ClientASN," +
	"ClientRequestHost,ClientRequestMethod,ClientRequestPath,ClientRequestQuery," +
	"ClientRequestProtocol,WAFAction,WAFRuleID," +
	"FirewallMatchesActions,FirewallMatchesSources,FirewallMatchesRuleIDs," +
	"EdgeResponseStatus,UserAgent"

var lookupCmd = &cobra.Command{
	Use:   "lookup <ray-id>",
	Short: "Look up a Cloudflare Ray ID",
	Long: `Look up a Cloudflare Ray ID to retrieve the action taken,
firewall rule matched, client details, and full request metadata.

The Ray ID is visible in the CF-Ray response header on any request
proxied through Cloudflare.

Uses the Cloudflare Logpull REST API (GET /zones/{zone_id}/logs/rayids/{id}).
Either --zone or --domain is required to scope the query.

Note: Logpull requires the Logs permission on your API token.

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

	if lookupZone == "" && lookupDomain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return err
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	zoneID, err := client.ResolveZoneID(cmd.Context(), cfClient, lookupZone, lookupDomain)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Looking up Ray ID %s…", rayID)

	entries, err := fetchRayID(cmd.Context(), cfClient, zoneID, rayID)
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
		p.Info("The ray ID may be outside the retention window, or your token may need the Logs permission.")
		return nil
	}

	for i, e := range entries {
		if len(entries) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "\nEntry %d of %d\n", i+1, len(entries))
		}
		url := e.ClientRequestHost + e.ClientRequestPath
		if e.ClientRequestQuery != "" {
			url += "?" + e.ClientRequestQuery
		}
		p.KV([][2]string{
			{"Ray ID", e.RayID},
			{"Timestamp", e.EdgeStartTimestamp},
			{"Status", fmt.Sprintf("%d", e.EdgeResponseStatus)},
			{"WAF Action", e.WAFAction},
			{"WAF Rule ID", e.WAFRuleID},
			{"FW Actions", strings.Join(e.FirewallMatchesActions, ", ")},
			{"FW Sources", strings.Join(e.FirewallMatchesSources, ", ")},
			{"FW Rule IDs", strings.Join(e.FirewallMatchesRuleIDs, ", ")},
			{"Client IP", e.ClientIP},
			{"Country", e.ClientCountry},
			{"ASN", fmt.Sprintf("%d", e.ClientASN)},
			{"Method", e.ClientRequestMethod},
			{"Protocol", e.ClientRequestProtocol},
			{"Host", e.ClientRequestHost},
			{"Path", e.ClientRequestPath},
			{"Query", e.ClientRequestQuery},
			{"User Agent", e.UserAgent},
			{"URL", url},
		})
	}
	return nil
}

// fetchRayID queries the Cloudflare Logpull REST API for log entries matching
// the given Ray ID. The response is NDJSON — one JSON object per line.
func fetchRayID(ctx context.Context, cfClient *cf.Client, zoneID, rayID string) ([]LogEntry, error) {
	raw, err := cfClient.Logs.RayID.Get(ctx, rayID, cflogs.RayIDGetParams{
		ZoneID:     cf.F(zoneID),
		Fields:     cf.F(logpullFields),
		Timestamps: cf.F(cflogs.RayIDGetParamsTimestampsRfc3339),
	})
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}

	// The Logpull API returns NDJSON. The SDK deserialises it into any.
	// Marshal back to bytes and parse as NDJSON (one JSON object per line).
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("re-marshalling response: %w", err)
	}

	return parseNDJSON(b)
}

// parseNDJSON parses a byte slice that may be:
//   - A JSON array (SDK bundled the NDJSON into an array), or
//   - A single JSON object (only one log line), or
//   - Raw NDJSON (newline-separated JSON objects).
func parseNDJSON(b []byte) ([]LogEntry, error) {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}

	// Try array first.
	if strings.HasPrefix(trimmed, "[") {
		var entries []LogEntry
		if err := json.Unmarshal(b, &entries); err == nil {
			return entries, nil
		}
		// Array of raw maps.
		var raw []map[string]any
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("parsing array response: %w", err)
		}
		return mapsToEntries(raw), nil
	}

	// Try single object.
	if strings.HasPrefix(trimmed, "{") {
		var entry LogEntry
		if err := json.Unmarshal(b, &entry); err == nil {
			return []LogEntry{entry}, nil
		}
	}

	// Fall back: line-by-line NDJSON.
	var entries []LogEntry
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e LogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parsing NDJSON line %q: %w", line, err)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func mapsToEntries(raw []map[string]any) []LogEntry {
	entries := make([]LogEntry, 0, len(raw))
	for _, m := range raw {
		b, _ := json.Marshal(m)
		var e LogEntry
		_ = json.Unmarshal(b, &e)
		entries = append(entries, e)
	}
	return entries
}

// Ensure time is imported even if not directly used in struct (EdgeStartTimestamp is a string).
var _ = time.RFC3339
