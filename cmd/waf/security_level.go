package waf

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/output"
)

const securityLevelSetting = "security_level"

var (
	securityLevelZone   string
	securityLevelDomain string
	securityLevelSet    string
	securityLevelDryRun bool
)

// validSecurityLevels maps the accepted --set values to the SDK enum.
var validSecurityLevels = map[string]zones.SecurityLevelValue{
	"off":             zones.SecurityLevelValueOff,
	"essentially_off": zones.SecurityLevelValueEssentiallyOff,
	"low":             zones.SecurityLevelValueLow,
	"medium":          zones.SecurityLevelValueMedium,
	"high":            zones.SecurityLevelValueHigh,
	"under_attack":    zones.SecurityLevelValueUnderAttack,
}

// securityLevelResult is the serialisable result for read and write alike.
type securityLevelResult struct {
	ZoneID     string `json:"zone_id"               toon:"zone_id"`
	Setting    string `json:"setting"               toon:"setting"`
	Value      string `json:"value"                 toon:"value"`
	Editable   string `json:"editable,omitempty"    toon:"editable,omitempty"`
	ModifiedOn string `json:"modified_on,omitempty" toon:"modified_on,omitempty"`
	Changed    bool   `json:"changed"               toon:"changed"`
}

var securityLevelCmd = &cobra.Command{
	Use:   "security-level",
	Short: "Read or set the zone security level",
	Long: `Read the zone's security level, or change it with --set.

Levels: ` + securityLevelList() + `

--set under_attack turns on I'm Under Attack mode, which interstitials every
visitor and takes effect the moment the call returns. Rehearse it with --dry-run
first if you are not certain of the zone.

Examples:
  cf waf security-level --domain example.com
  cf waf security-level --zone ZONE_ID --json
  cf waf security-level --domain example.com --set under_attack --dry-run
  cf waf security-level --domain example.com --set high`,
	RunE: runSecurityLevel,
}

func init() {
	securityLevelCmd.Flags().StringVar(&securityLevelZone, "zone", "", "Zone ID")
	securityLevelCmd.Flags().StringVar(&securityLevelDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	securityLevelCmd.Flags().StringVar(&securityLevelSet, "set", "", "New security level: "+securityLevelList())
	securityLevelCmd.Flags().BoolVar(&securityLevelDryRun, "dry-run", false, "Show what would change without calling the API")

	securityLevelCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runSecurityLevel(cmd *cobra.Command, _ []string) error {
	// Validate the level before resolving the zone so a typo costs no API call.
	var level zones.SecurityLevelValue
	if securityLevelSet != "" {
		var ok bool
		level, ok = validSecurityLevels[strings.ToLower(securityLevelSet)]
		if !ok {
			p, _ := cmdutil.Setup(cmd)
			err := fmt.Errorf("invalid --set %q; valid values: %s", securityLevelSet, securityLevelList())
			p.Error("%v", err)
			return err
		}
	}

	cx, err := cmdutil.Zone(cmd, securityLevelZone, securityLevelDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	if securityLevelSet == "" {
		res, err := cx.Client.Zones.Settings.Get(cmd.Context(), securityLevelSetting,
			zones.SettingGetParams{ZoneID: cf.F(cx.ZoneID)})
		if err != nil {
			p.Error("API error: %v", err)
			return err
		}
		return reportSecurityLevel(cmd, p, securityLevelResult{
			ZoneID:     cx.ZoneID,
			Setting:    securityLevelSetting,
			Value:      fmt.Sprintf("%v", res.Value),
			Editable:   fmt.Sprintf("%v", res.Editable),
			ModifiedOn: res.ModifiedOn.String(),
			Changed:    false,
		})
	}

	if cmdutil.DryRun(p, securityLevelDryRun,
		"set security level to %s for zone %s", level, cx.ZoneID) {
		return nil
	}

	p.Info("Setting security level to %s for zone %s…", level, cx.ZoneID)

	res, err := cx.Client.Zones.Settings.Edit(cmd.Context(), securityLevelSetting, zones.SettingEditParams{
		ZoneID: cf.F(cx.ZoneID),
		Body: zones.SettingEditParamsBody{
			Value: cf.F[interface{}](string(level)),
		},
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	return reportSecurityLevel(cmd, p, securityLevelResult{
		ZoneID:     cx.ZoneID,
		Setting:    securityLevelSetting,
		Value:      fmt.Sprintf("%v", res.Value),
		Editable:   fmt.Sprintf("%v", res.Editable),
		ModifiedOn: res.ModifiedOn.String(),
		Changed:    true,
	})
}

// reportSecurityLevel emits the result in whichever output mode is active.
func reportSecurityLevel(cmd *cobra.Command, p *output.Printer, result securityLevelResult) error {
	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if result.Changed {
		p.Success("Security level updated")
	}
	p.KV([][2]string{
		{"Zone", result.ZoneID},
		{"Setting", result.Setting},
		{"Value", result.Value},
		{"Editable", result.Editable},
		{"Modified", result.ModifiedOn},
	})
	_ = cmd
	return nil
}

// securityLevelOrder lists the levels least to most aggressive, for help text.
var securityLevelOrder = []string{"off", "essentially_off", "low", "medium", "high", "under_attack"}

// securityLevelList renders the accepted levels in Cloudflare's own order.
func securityLevelList() string {
	return strings.Join(securityLevelOrder, ", ")
}
