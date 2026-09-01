package customhostnames

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	updateZone     string
	updateDomain   string
	updateHostname string
	updateMinTLS   string
	updateCA       string
	updateDryRun   bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the SSL settings on a custom hostname",
	Long: `Update the SSL settings on an existing custom hostname, looked up by name.

Examples:
  cf custom-hostnames update --domain example.com --hostname learn.customer.com --min-tls-version 1.3
  cf custom-hostnames update --domain example.com --hostname learn.customer.com --certificate-authority google
  cf custom-hostnames update --domain example.com --hostname learn.customer.com --min-tls-version 1.3 --dry-run`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().StringVar(&updateZone, "zone", "", "Zone ID")
	updateCmd.Flags().StringVar(&updateDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	updateCmd.Flags().StringVar(&updateHostname, "hostname", "", "Custom hostname to update (required)")
	// No defaults here on purpose: update sends only the fields the caller
	// actually passed, so changing the CA does not silently rewrite the TLS
	// floor to 1.2 (and vice versa).
	updateCmd.Flags().StringVar(&updateMinTLS, "min-tls-version", "", "Minimum TLS version: 1.0, 1.1, 1.2, 1.3")
	updateCmd.Flags().StringVar(&updateCA, "certificate-authority", "", "Certificate authority: lets_encrypt, google")
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "Show what would change without calling the API")

	updateCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = updateCmd.MarkFlagRequired("hostname")
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, updateZone, updateDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	if !cmd.Flags().Changed("min-tls-version") && !cmd.Flags().Changed("certificate-authority") {
		err := fmt.Errorf("nothing to update: pass --min-tls-version, --certificate-authority, or both")
		p.Error("%v", err)
		return err
	}

	sslParams := custom_hostnames.CustomHostnameEditParamsSSL{
		Type: cf.F(custom_hostnames.DomainValidationTypeDv),
	}
	changes := []string{}

	if cmd.Flags().Changed("min-tls-version") {
		minTLS, err := parseMinTLSVersion(updateMinTLS)
		if err != nil {
			p.Error("%v", err)
			return err
		}
		sslParams.Settings = cf.F(custom_hostnames.CustomHostnameEditParamsSSLSettings{
			MinTLSVersion: cf.F(custom_hostnames.CustomHostnameEditParamsSSLSettingsMinTLSVersion(minTLS)),
		})
		changes = append(changes, "min_tls="+string(minTLS))
	}

	if cmd.Flags().Changed("certificate-authority") {
		ca, err := parseCertificateAuthority(updateCA)
		if err != nil {
			p.Error("%v", err)
			return err
		}
		sslParams.CertificateAuthority = cf.F(ca)
		changes = append(changes, "ca="+string(ca))
	}

	p.Info("Looking up %s in zone %s…", updateHostname, ctx.ZoneID)
	existing, err := findByHostname(cmd.Context(), ctx.Client, ctx.ZoneID, updateHostname)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	if cmdutil.DryRun(p, updateDryRun,
		"update custom hostname %s (%s): %s",
		existing.Hostname, existing.ID, strings.Join(changes, ", ")) {
		return nil
	}

	p.Info("Updating custom hostname %s…", existing.ID)

	resp, err := ctx.Client.CustomHostnames.Edit(cmd.Context(), existing.ID, custom_hostnames.CustomHostnameEditParams{
		ZoneID: cf.F(ctx.ZoneID),
		SSL:    cf.F(sslParams),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := fromEditResponse(resp)

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Custom hostname updated")
	p.KV([][2]string{
		{"Hostname", result.Hostname},
		{"ID", result.ID},
		{"Status", dash(result.Status)},
		{"SSL Status", dash(result.SSLStatus)},
		{"SSL Method", dash(result.SSLMethod)},
		{"Min TLS Version", dash(result.MinTLSVersion)},
		{"Certificate Authority", dash(result.CertificateAuthority)},
	})
	return nil
}
