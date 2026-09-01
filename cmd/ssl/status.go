package ssl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/ssl"
	"github.com/cloudflare/cloudflare-go/v6/zones"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	statusZone   string
	statusDomain string
)

// certificatePackResult is the serialisable result for a single certificate pack.
type certificatePackResult struct {
	ID           string   `json:"id"            toon:"id"`
	Type         string   `json:"type"           toon:"type"`
	Status       string   `json:"status"         toon:"status"`
	Hosts        []string `json:"hosts"          toon:"hosts"`
	ValidityDays int      `json:"validity_days"  toon:"validity_days"`
}

// statusResult is the top-level result for --json / --toon output.
type statusResult struct {
	ZoneID           string                  `json:"zone_id"           toon:"zone_id"`
	SSLMode          string                  `json:"ssl_mode"          toon:"ssl_mode"`
	MinTLSVersion    string                  `json:"min_tls_version"   toon:"min_tls_version"`
	AlwaysUseHTTPS   string                  `json:"always_use_https"  toon:"always_use_https"`
	CertificatePacks []certificatePackResult `json:"certificate_packs" toon:"certificate_packs"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show SSL/TLS status for a zone",
	Long: `Gather and display SSL/TLS mode, minimum TLS version, always-use-HTTPS,
and certificate pack details for a Cloudflare zone in one view.

Examples:
  cf ssl status --zone ZONE_ID
  cf ssl status --domain example.com --json`,
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
	ctx := cmd.Context()

	p.Info("Fetching SSL/TLS status for zone %s…", c.ZoneID)

	sslSetting, err := c.Client.Zones.Settings.Get(ctx, "ssl", zones.SettingGetParams{
		ZoneID: cf.F(c.ZoneID),
	})
	if err != nil {
		p.Error("API error fetching ssl setting: %v", err)
		return err
	}

	minTLSSetting, err := c.Client.Zones.Settings.Get(ctx, "min_tls_version", zones.SettingGetParams{
		ZoneID: cf.F(c.ZoneID),
	})
	if err != nil {
		p.Error("API error fetching min_tls_version setting: %v", err)
		return err
	}

	alwaysUseHTTPS := "N/A"
	alwaysUseHTTPSSetting, err := c.Client.Zones.Settings.Get(ctx, "always_use_https", zones.SettingGetParams{
		ZoneID: cf.F(c.ZoneID),
	})
	if err != nil {
		p.Info("Could not fetch always_use_https setting (likely missing permission): %v", err)
	} else {
		alwaysUseHTTPS = settingValueString(alwaysUseHTTPSSetting.Value)
	}

	var packs []certificatePackResult
	iter := c.Client.SSL.CertificatePacks.ListAutoPaging(ctx, ssl.CertificatePackListParams{
		ZoneID: cf.F(c.ZoneID),
	})
	for iter.Next() {
		pack := iter.Current()
		packs = append(packs, certificatePackResult{
			ID:           pack.ID,
			Type:         string(pack.Type),
			Status:       string(pack.Status),
			Hosts:        pack.Hosts,
			ValidityDays: int(pack.ValidityDays),
		})
	}
	if err := iter.Err(); err != nil {
		p.Error("API error listing certificate packs: %v", err)
		return err
	}

	result := statusResult{
		ZoneID:           c.ZoneID,
		SSLMode:          settingValueString(sslSetting.Value),
		MinTLSVersion:    settingValueString(minTLSSetting.Value),
		AlwaysUseHTTPS:   alwaysUseHTTPS,
		CertificatePacks: packs,
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.KV([][2]string{
		{"Zone ID", result.ZoneID},
		{"SSL Mode", result.SSLMode},
		{"Min TLS Version", result.MinTLSVersion},
		{"Always Use HTTPS", result.AlwaysUseHTTPS},
	})

	if len(packs) == 0 {
		p.Info("No certificate packs found.")
		return nil
	}

	rows := make([][]string, 0, len(packs))
	for _, pk := range packs {
		rows = append(rows, []string{
			pk.ID,
			pk.Type,
			pk.Status,
			strconv.Itoa(pk.ValidityDays),
			strings.Join(pk.Hosts, ", "),
		})
	}

	fmt.Fprintln(cmd.OutOrStdout())
	p.Table(
		[]string{"ID", "TYPE", "STATUS", "VALIDITY DAYS", "HOSTS"},
		rows,
	)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d certificate pack(s)\n", len(packs))
	return nil
}

// settingValueString renders a zone setting's interface{} value (which arrives
// as a concrete string-kind type such as SettingGetResponseZonesSchemasSSLValue
// or MinTLSVersionValue) as a plain string.
func settingValueString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
