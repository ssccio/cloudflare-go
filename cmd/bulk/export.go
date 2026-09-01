package bulk

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	exportZone   string
	exportDomain string
	exportOutput string
)

// exportResult is the machine-readable export payload.
type exportResult struct {
	Zone      string   `json:"zone"      toon:"zone"`
	Exported  string   `json:"exported"  toon:"exported"`
	Total     int      `json:"total"     toon:"total"`
	Hostnames []string `json:"hostnames" toon:"hostnames"`
}

var exportHostnamesCmd = &cobra.Command{
	Use:   "export-hostnames",
	Short: "Export all custom hostnames in a zone to a file or stdout",
	Long: `Export every Cloudflare for SaaS custom hostname in a zone, sorted, one per
line, preceded by comment lines giving the zone, the UTC export timestamp, and
the total. The output is in the format import-hostnames reads.

With no --output the list is written to stdout. In --json or --toon mode the
hostname list is emitted as structured data instead.

Examples:
  cf bulk export-hostnames --zone ZONE_ID
  cf bulk export-hostnames --domain example.com --output hostnames.txt
  cf bulk export-hostnames --zone ZONE_ID --json`,
	RunE: runExportHostnames,
}

func init() {
	exportHostnamesCmd.Flags().StringVar(&exportZone, "zone", "", "Zone ID")
	exportHostnamesCmd.Flags().StringVar(&exportDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	exportHostnamesCmd.Flags().StringVar(&exportOutput, "output", "", "Write to this file instead of stdout")

	exportHostnamesCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runExportHostnames(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, exportZone, exportDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	p.Info("Fetching custom hostnames for zone %s…", ctx.ZoneID)

	records, err := listCustomHostnames(cmd.Context(), ctx.Client, ctx.ZoneID)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	hostnames := sortedHostnames(records)
	exported := time.Now().UTC().Format(time.RFC3339)

	if p.JSON || p.TOON {
		p.PrintResult(exportResult{
			Zone:      ctx.ZoneID,
			Exported:  exported,
			Total:     len(hostnames),
			Hostnames: hostnames,
		})
		return nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Custom hostnames for zone %s\n", ctx.ZoneID)
	fmt.Fprintf(&b, "# Exported: %s\n", exported)
	fmt.Fprintf(&b, "# Total: %d\n", len(hostnames))
	for _, h := range hostnames {
		fmt.Fprintf(&b, "%s\n", h)
	}

	if exportOutput == "" {
		fmt.Fprint(p.Out, b.String())
		return nil
	}

	if err := os.WriteFile(exportOutput, []byte(b.String()), 0o644); err != nil {
		p.Error("writing %s: %v", exportOutput, err)
		return err
	}
	p.Success("Exported %d hostname(s) to %s", len(hostnames), exportOutput)
	return nil
}
