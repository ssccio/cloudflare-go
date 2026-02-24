package dns

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"

	"github.com/ssccio/cloudflare-go/internal/client"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	listZone       string
	listDomain     string
	listFilterType string
	listFilterName string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List DNS records in a zone",
	Long: `List all DNS records in a Cloudflare zone, with optional type and name filters.

Examples:
  cf dns list --zone ZONE_ID
  cf dns list --zone ZONE_ID --type A
  cf dns list --zone ZONE_ID --name example.com
  cf dns list --zone ZONE_ID --json`,
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVar(&listZone, "zone", "", "Zone ID")
	listCmd.Flags().StringVar(&listDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	listCmd.Flags().StringVar(&listFilterType, "type", "", "Filter by record type (A, AAAA, CNAME, MX, TXT, NS)")
	listCmd.Flags().StringVar(&listFilterName, "name", "", "Filter by record name")

	listCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runList(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")

	p := output.New(jsonFlag, quiet, noColor)

	if listZone == "" && listDomain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return err
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	zoneID, err := client.ResolveZoneID(cmd.Context(), cfClient, listZone, listDomain)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Fetching DNS records for zone %s…", zoneID)

	params := dns.RecordListParams{
		ZoneID: cf.F(zoneID),
	}
	if listFilterType != "" {
		params.Type = cf.F(dns.RecordListParamsType(strings.ToUpper(listFilterType)))
	}
	if listFilterName != "" {
		params.Name = cf.F(dns.RecordListParamsName{
			Exact: cf.F(listFilterName),
		})
	}

	var records []dnsRecordResult
	iter := cfClient.DNS.Records.ListAutoPaging(context.Background(), params)
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
			ModifiedOn: r.ModifiedOn.String(),
		})
	}
	if err := iter.Err(); err != nil {
		p.Error("API error: %v", err)
		return err
	}

	if jsonFlag {
		p.PrintJSON(records)
		return nil
	}

	if len(records) == 0 {
		p.Info("No DNS records found.")
		return nil
	}

	rows := make([][]string, 0, len(records))
	for _, r := range records {
		proxied := "—"
		if r.Proxied {
			proxied = "✓"
		}
		rows = append(rows, []string{
			r.Name,
			r.Type,
			r.Content,
			ttlDisplay(r.TTL),
			proxied,
			r.ID,
		})
	}

	p.Table(
		[]string{"NAME", "TYPE", "CONTENT", "TTL", "PROXIED", "ID"},
		rows,
	)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d record(s)\n", len(records))
	return nil
}
