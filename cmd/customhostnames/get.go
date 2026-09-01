package customhostnames

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	getZone     string
	getDomain   string
	getHostname string
)

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Show one custom hostname by name",
	Long: `Show the full detail for a single custom hostname, including the DCV
validation records the customer must publish and any validation errors.

Examples:
  cf custom-hostnames get --domain example.com --hostname learn.customer.com
  cf custom-hostnames get --zone ZONE_ID --hostname learn.customer.com --json`,
	RunE: runGet,
}

func init() {
	getCmd.Flags().StringVar(&getZone, "zone", "", "Zone ID")
	getCmd.Flags().StringVar(&getDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	getCmd.Flags().StringVar(&getHostname, "hostname", "", "Custom hostname to look up (required)")

	getCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = getCmd.MarkFlagRequired("hostname")
}

func runGet(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, getZone, getDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	p.Info("Looking up %s in zone %s…", getHostname, ctx.ZoneID)

	h, err := findByHostname(cmd.Context(), ctx.Client, ctx.ZoneID, getHostname)
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
		{"ID", h.ID},
		{"Status", dash(h.Status)},
		{"SSL Status", dash(h.SSLStatus)},
		{"SSL Method", dash(h.SSLMethod)},
		{"Created", dash(h.CreatedAt)},
	})
	printValidation(cmd, p, h)
	return nil
}

// printValidation renders the DCV records and any validation errors.
func printValidation(cmd *cobra.Command, p *output.Printer, h customHostnameResult) {
	if len(h.ValidationRecords) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nSSL validation records:\n")
		for _, vr := range h.ValidationRecords {
			var pairs [][2]string
			if vr.TXTName != "" {
				pairs = append(pairs, [2]string{"TXT Name", vr.TXTName})
			}
			if vr.TXTValue != "" {
				pairs = append(pairs, [2]string{"TXT Value", vr.TXTValue})
			}
			if vr.HTTPURL != "" {
				pairs = append(pairs, [2]string{"HTTP URL", vr.HTTPURL})
			}
			if vr.HTTPBody != "" {
				pairs = append(pairs, [2]string{"HTTP Body", vr.HTTPBody})
			}
			for _, e := range vr.Emails {
				pairs = append(pairs, [2]string{"Email", e})
			}
			p.KV(pairs)
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}

	if h.Ownership != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Ownership verification record:\n")
		p.KV([][2]string{
			{"Name", h.Ownership.Name},
			{"Type", h.Ownership.Type},
			{"Value", h.Ownership.Value},
		})
		fmt.Fprintln(cmd.OutOrStdout())
	}

	if h.OwnershipHTTP != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Ownership verification (HTTP):\n")
		p.KV([][2]string{
			{"URL", h.OwnershipHTTP.HTTPURL},
			{"Body", h.OwnershipHTTP.HTTPBody},
		})
		fmt.Fprintln(cmd.OutOrStdout())
	}

	if len(h.ValidationErrors) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Validation errors:\n")
		for _, e := range h.ValidationErrors {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", e)
		}
	}
	if len(h.VerificationErrors) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Verification errors:\n")
		for _, e := range h.VerificationErrors {
			fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", e)
		}
	}
}
