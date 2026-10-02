package bots

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/bot_management"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	statusZone   string
	statusDomain string
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show a zone's Bot Management configuration",
	Long: `Show the zone's bot_management settings as the API returns them: whether
Enterprise Bot Management is active, Bot Fight Mode / Super Bot Fight Mode
settings, JavaScript detections, the AI crawler setting and the model version.

Needs Zone → Bot Management → Read on the token.

Examples:
  cf bots status --domain example.com
  cf bots status --domain example.com --json`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().StringVar(&statusZone, "zone", "", "Zone ID")
	statusCmd.Flags().StringVar(&statusDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	statusCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runStatus(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, statusZone, statusDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	res, err := c.Client.BotManagement.Get(cmd.Context(), bot_management.BotManagementGetParams{
		ZoneID: cf.F(c.ZoneID),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	// The response is a union across plan types, so keep whatever fields the
	// API sent rather than a fixed struct.
	settings := map[string]any{}
	if err := json.Unmarshal([]byte(res.JSON.RawJSON()), &settings); err != nil {
		p.Error("decoding response: %v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(map[string]any{"zone_id": c.ZoneID, "settings": settings})
		return nil
	}

	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := [][2]string{{"zone_id", c.ZoneID}}
	for _, k := range keys {
		pairs = append(pairs, [2]string{k, fmt.Sprint(settings[k])})
	}
	p.KV(pairs)
	return nil
}
