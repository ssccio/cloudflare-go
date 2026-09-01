package zones

import (
	"fmt"
	"sort"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/zones"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

// commonSettings is the set of zone settings worth showing by default. The
// zone settings API has no "list everything" call, so each one is a request.
var commonSettings = []string{
	"ssl",
	"min_tls_version",
	"security_level",
	"always_use_https",
	"automatic_https_rewrites",
	"http2",
	"http3",
	"0rtt",
	"brotli",
	"cache_level",
	"browser_cache_ttl",
	"development_mode",
	"hotlink_protection",
	"ip_geolocation",
	"ipv6",
	"websockets",
}

var (
	settingsZone   string
	settingsDomain string
	settingsOnly   []string
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Show zone settings",
	Long: `Show common zone settings (SSL mode, TLS floor, security level, caching,
protocol support). A setting the token cannot read is reported as an error
against that key rather than failing the whole command.

Examples:
  cf zones settings --domain example.com
  cf zones settings --domain example.com --setting ssl --setting min_tls_version
  cf zones settings --zone ZONE_ID --json --query 'ssl'`,
	RunE: runSettings,
}

func init() {
	settingsCmd.Flags().StringVar(&settingsZone, "zone", "", "Zone ID")
	settingsCmd.Flags().StringVar(&settingsDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	settingsCmd.Flags().StringArrayVar(&settingsOnly, "setting", nil,
		"Fetch only this setting (repeatable); defaults to a common set")
	settingsCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runSettings(cmd *cobra.Command, _ []string) error {
	p, token := cmdutil.Setup(cmd)

	if settingsZone == "" && settingsDomain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return err
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	zoneID, err := client.ResolveZoneID(cmd.Context(), cfClient, settingsZone, settingsDomain)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	keys := settingsOnly
	if len(keys) == 0 {
		keys = commonSettings
	}

	p.Info("Fetching %d settings for zone %s…", len(keys), zoneID)

	values := make(map[string]any, len(keys))
	for _, key := range keys {
		res, err := cfClient.Zones.Settings.Get(cmd.Context(), key, zones.SettingGetParams{
			ZoneID: cf.F(zoneID),
		})
		if err != nil {
			values[key] = nil
			p.Info("  %s: unavailable (%v)", key, err)
			continue
		}
		values[key] = res.Value
	}

	if p.JSON || p.TOON {
		p.PrintResult(values)
		return nil
	}

	sorted := make([]string, 0, len(values))
	for k := range values {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	rows := make([][]string, 0, len(sorted))
	for _, k := range sorted {
		rows = append(rows, []string{k, settingValue(values[k])})
	}
	p.Table([]string{"SETTING", "VALUE"}, rows)
	return nil
}

// settingValue renders a setting value, which the API types as a union.
func settingValue(v any) string {
	if v == nil {
		return "(unavailable)"
	}
	return fmt.Sprintf("%v", v)
}
