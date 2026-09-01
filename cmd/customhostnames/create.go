package customhostnames

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	createZone      string
	createDomain    string
	createHostname  string
	createSSLMethod string
	createMinTLS    string
	createCA        string
	createDryRun    bool
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a custom hostname",
	Long: `Create a custom hostname and order a domain-validated certificate for it.

The customer must publish the returned validation records at their DNS provider
before Cloudflare will issue the certificate.

Examples:
  cf custom-hostnames create --domain example.com --hostname learn.customer.com
  cf custom-hostnames create --domain example.com --hostname learn.customer.com --ssl-method txt
  cf custom-hostnames create --domain example.com --hostname learn.customer.com --min-tls-version 1.3
  cf custom-hostnames create --domain example.com --hostname learn.customer.com --dry-run`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createZone, "zone", "", "Zone ID")
	createCmd.Flags().StringVar(&createDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	createCmd.Flags().StringVar(&createHostname, "hostname", "", "Custom hostname to create (required)")
	createCmd.Flags().StringVar(&createSSLMethod, "ssl-method", "http", "DCV method: http, txt, cname")
	createCmd.Flags().StringVar(&createMinTLS, "min-tls-version", "1.2", "Minimum TLS version: 1.0, 1.1, 1.2, 1.3")
	createCmd.Flags().StringVar(&createCA, "certificate-authority", "lets_encrypt", "Certificate authority: lets_encrypt, google")
	createCmd.Flags().BoolVar(&createDryRun, "dry-run", false, "Show what would be created without calling the API")

	createCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = createCmd.MarkFlagRequired("hostname")
}

func runCreate(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, createZone, createDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	method, err := parseSSLMethod(createSSLMethod)
	if err != nil {
		p.Error("%v", err)
		return err
	}
	minTLS, err := parseMinTLSVersion(createMinTLS)
	if err != nil {
		p.Error("%v", err)
		return err
	}
	ca, err := parseCertificateAuthority(createCA)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	if cmdutil.DryRun(p, createDryRun,
		"create custom hostname %s in zone %s (method=%s, min_tls=%s, ca=%s)",
		createHostname, ctx.ZoneID, method, minTLS, ca) {
		return nil
	}

	p.Info("Creating custom hostname %s in zone %s…", createHostname, ctx.ZoneID)

	resp, err := ctx.Client.CustomHostnames.New(cmd.Context(), custom_hostnames.CustomHostnameNewParams{
		ZoneID:   cf.F(ctx.ZoneID),
		Hostname: cf.F(createHostname),
		SSL: cf.F(custom_hostnames.CustomHostnameNewParamsSSL{
			Method:               cf.F(method),
			Type:                 cf.F(custom_hostnames.DomainValidationTypeDv),
			CertificateAuthority: cf.F(ca),
			Settings: cf.F(custom_hostnames.CustomHostnameNewParamsSSLSettings{
				MinTLSVersion: cf.F(custom_hostnames.CustomHostnameNewParamsSSLSettingsMinTLSVersion(minTLS)),
			}),
		}),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := fromNewResponse(resp)

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Custom hostname created")
	p.KV([][2]string{
		{"Hostname", result.Hostname},
		{"ID", result.ID},
		{"Status", dash(result.Status)},
		{"SSL Status", dash(result.SSLStatus)},
		{"SSL Method", dash(result.SSLMethod)},
	})
	if len(result.ValidationRecords) > 0 || result.Ownership != nil || result.OwnershipHTTP != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "\nThe customer must add these records at their DNS provider:\n")
	}
	printValidation(cmd, p, result)
	return nil
}
