package dns

import (
	"fmt"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	updateZone    string
	updateDomain  string
	updateID      string
	updateContent string
	updateTTL     int
	updateProxied bool
	updateComment string
	updateDryRun  bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update an existing DNS record",
	Long: `Update the content, TTL, proxy status, or comment of an existing DNS record.

Only the flags you pass are changed; everything else on the record is left as is.

Examples:
  cf dns update --domain example.com --id RECORD_ID --content 203.0.113.10
  cf dns update --domain example.com --id RECORD_ID --ttl 300 --proxied
  cf dns update --zone ZONE_ID --id RECORD_ID --content 203.0.113.10 --dry-run`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().StringVar(&updateZone, "zone", "", "Zone ID")
	updateCmd.Flags().StringVar(&updateDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	updateCmd.Flags().StringVar(&updateID, "id", "", "DNS record ID (required)")
	updateCmd.Flags().StringVar(&updateContent, "content", "", "New record content")
	updateCmd.Flags().IntVar(&updateTTL, "ttl", 0, "New TTL in seconds (1 = automatic)")
	updateCmd.Flags().BoolVar(&updateProxied, "proxied", false, "Proxy through Cloudflare")
	updateCmd.Flags().StringVar(&updateComment, "comment", "", "New record comment")
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "Preview the change without applying it")

	updateCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = updateCmd.MarkFlagRequired("id")
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, updateZone, updateDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	changed := []string{}
	params := dns.RecordEditParams{ZoneID: cf.F(ctx.ZoneID)}
	body := dns.RecordEditParamsBody{}

	if cmd.Flags().Changed("content") {
		body.Content = cf.F(updateContent)
		changed = append(changed, "content="+updateContent)
	}
	if cmd.Flags().Changed("ttl") {
		body.TTL = cf.F(dns.TTL(updateTTL))
		changed = append(changed, fmt.Sprintf("ttl=%d", updateTTL))
	}
	if cmd.Flags().Changed("proxied") {
		body.Proxied = cf.F(updateProxied)
		changed = append(changed, fmt.Sprintf("proxied=%t", updateProxied))
	}
	if cmd.Flags().Changed("comment") {
		body.Comment = cf.F(updateComment)
		changed = append(changed, "comment="+updateComment)
	}

	if len(changed) == 0 {
		err := fmt.Errorf("nothing to update: pass at least one of --content, --ttl, --proxied, --comment")
		p.Error("%v", err)
		return err
	}
	params.Body = body

	if cmdutil.DryRun(p, updateDryRun, "update record %s: %s", updateID, strings.Join(changed, ", ")) {
		return nil
	}

	resp, err := ctx.Client.DNS.Records.Edit(cmd.Context(), updateID, params)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := dnsRecordResult{
		ID:         resp.ID,
		Name:       resp.Name,
		Type:       string(resp.Type),
		Content:    resp.Content,
		TTL:        int(resp.TTL),
		Proxied:    resp.Proxied,
		Comment:    resp.Comment,
		ModifiedOn: resp.ModifiedOn.String(),
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Updated %s record %s", result.Type, result.Name)
	p.KV([][2]string{
		{"ID", result.ID},
		{"Content", result.Content},
		{"TTL", ttlDisplay(result.TTL)},
		{"Proxied", fmt.Sprintf("%t", result.Proxied)},
	})
	return nil
}
