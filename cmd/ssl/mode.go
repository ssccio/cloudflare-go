package ssl

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	modeZone   string
	modeDomain string
	modeSet    string
	modeDryRun bool
)

var validSSLModes = []string{"off", "flexible", "full", "strict"}

// modeResult is the result for --json / --toon output.
type modeResult struct {
	ZoneID  string `json:"zone_id" toon:"zone_id"`
	SSLMode string `json:"ssl_mode" toon:"ssl_mode"`
}

var modeCmd = &cobra.Command{
	Use:   "mode",
	Short: "Get or set the SSL/TLS encryption mode for a zone",
	Long: `Read or update the SSL/TLS encryption mode (the "ssl" zone setting).

Valid modes: off, flexible, full, strict

Examples:
  cf ssl mode --zone ZONE_ID
  cf ssl mode --zone ZONE_ID --set strict
  cf ssl mode --domain example.com --set full --dry-run`,
	RunE: runMode,
}

func init() {
	modeCmd.Flags().StringVar(&modeZone, "zone", "", "Zone ID")
	modeCmd.Flags().StringVar(&modeDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	modeCmd.Flags().StringVar(&modeSet, "set", "", "Set the SSL mode: off, flexible, full, strict")
	modeCmd.Flags().BoolVar(&modeDryRun, "dry-run", false, "Preview the change without applying it")
	modeCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runMode(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, modeZone, modeDomain)
	if err != nil {
		return err
	}
	p := c.Printer
	ctx := cmd.Context()

	if modeSet == "" {
		setting, err := c.Client.Zones.Settings.Get(ctx, "ssl", zones.SettingGetParams{
			ZoneID: cf.F(c.ZoneID),
		})
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		return printModeResult(cmd, p, c.ZoneID, settingValueString(setting.Value))
	}

	if !isValidSSLMode(modeSet) {
		err := fmt.Errorf("invalid --set value %q; valid values: %v", modeSet, validSSLModes)
		p.Error("%v", err)
		return err
	}

	if cmdutil.DryRun(p, modeDryRun, "set SSL mode to %q for zone %s", modeSet, c.ZoneID) {
		return nil
	}

	p.Info("Setting SSL mode to %q for zone %s…", modeSet, c.ZoneID)

	resp, err := c.Client.Zones.Settings.Edit(ctx, "ssl", zones.SettingEditParams{
		ZoneID: cf.F(c.ZoneID),
		Body: zones.SettingEditParamsBodyValue{
			Value: cf.Raw[zones.SettingEditParamsBodyValueValueUnion](modeSet),
		},
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	p.Success("SSL mode updated")
	return printModeResult(cmd, p, c.ZoneID, settingValueString(resp.Value))
}

func printModeResult(_ *cobra.Command, p *output.Printer, zoneID, mode string) error {
	result := modeResult{ZoneID: zoneID, SSLMode: mode}
	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}
	p.KV([][2]string{
		{"Zone ID", result.ZoneID},
		{"SSL Mode", result.SSLMode},
	})
	return nil
}

func isValidSSLMode(mode string) bool {
	for _, m := range validSSLModes {
		if mode == m {
			return true
		}
	}
	return false
}
