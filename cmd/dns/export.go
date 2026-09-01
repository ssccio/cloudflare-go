package dns

import (
	"encoding/json"
	"fmt"
	"os"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	exportZone   string
	exportDomain string
	exportOutput string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export all DNS records in a zone as JSON",
	Long: `Export every DNS record in a zone as a JSON array, for backup or diffing
before a change.

Examples:
  cf dns export --domain example.com
  cf dns export --domain example.com --output dns_backup.json`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVar(&exportZone, "zone", "", "Zone ID")
	exportCmd.Flags().StringVar(&exportDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	exportCmd.Flags().StringVar(&exportOutput, "output", "", "Write to this file instead of stdout")
	exportCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runExport(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, exportZone, exportDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	p.Info("Exporting DNS records for zone %s…", ctx.ZoneID)

	var records []dnsRecordResult
	iter := ctx.Client.DNS.Records.ListAutoPaging(cmd.Context(), dns.RecordListParams{
		ZoneID: cf.F(ctx.ZoneID),
	})
	for iter.Next() {
		r := iter.Current()
		records = append(records, dnsRecordResult{
			ID:         r.ID,
			Name:       r.Name,
			Type:       string(r.Type),
			Content:    r.Content,
			TTL:        int(r.TTL),
			Proxied:    r.Proxied,
			Comment:    r.Comment,
			CreatedOn:  r.CreatedOn.String(),
			ModifiedOn: r.ModifiedOn.String(),
		})
	}
	if err := iter.Err(); err != nil {
		p.Error("API error: %v", err)
		return err
	}

	if exportOutput == "" {
		p.PrintJSON(records)
		return nil
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		p.Error("encoding records: %v", err)
		return err
	}
	if err := os.WriteFile(exportOutput, append(data, '\n'), 0o644); err != nil {
		p.Error("writing %s: %v", exportOutput, err)
		return err
	}

	p.Success("Exported %d record(s) to %s", len(records), exportOutput)
	if p.JSON || p.TOON {
		p.PrintResult(map[string]any{"file": exportOutput, "count": len(records)})
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", exportOutput)
	}
	return nil
}
