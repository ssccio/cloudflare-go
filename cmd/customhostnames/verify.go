package customhostnames

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	verifyZone     string
	verifyDomain   string
	verifyHostname string
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check whether a custom hostname's certificate is active",
	Long: `Report the activation and SSL status of a custom hostname, and print the
DNS records still needed when the certificate is not yet active.

Examples:
  cf custom-hostnames verify --domain example.com --hostname learn.customer.com
  cf custom-hostnames verify --domain example.com --hostname learn.customer.com --json`,
	RunE: runVerify,
}

func init() {
	verifyCmd.Flags().StringVar(&verifyZone, "zone", "", "Zone ID")
	verifyCmd.Flags().StringVar(&verifyDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	verifyCmd.Flags().StringVar(&verifyHostname, "hostname", "", "Custom hostname to verify (required)")

	verifyCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = verifyCmd.MarkFlagRequired("hostname")
}

func runVerify(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, verifyZone, verifyDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	p.Info("Looking up %s in zone %s…", verifyHostname, ctx.ZoneID)

	h, err := findByHostname(cmd.Context(), ctx.Client, ctx.ZoneID, verifyHostname)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(h)
		return nil
	}

	p.KV([][2]string{
		{"Hostname", h.Hostname},
		{"Status", dash(h.Status)},
		{"SSL Status", dash(h.SSLStatus)},
		{"SSL Method", dash(h.SSLMethod)},
	})

	if len(h.ValidationErrors) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nValidation errors:\n")
		for _, e := range h.ValidationErrors {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", e)
		}
	}
	if len(h.VerificationErrors) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nVerification errors:\n")
		for _, e := range h.VerificationErrors {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", e)
		}
	}

	if strings.EqualFold(h.SSLStatus, "active") {
		fmt.Fprintf(cmd.OutOrStdout(), "\nCertificate is active, no action needed.\n")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nCertificate is not active. Required DNS records:\n")
	printValidation(cmd, p, h)
	return nil
}
