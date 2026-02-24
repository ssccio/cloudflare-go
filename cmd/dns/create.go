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
	createZone    string
	createDomain  string
	createName    string
	createType    string
	createContent string
	createTTL     int
	createProxied bool
	createComment string
)

// dnsRecordResult is the JSON-serialisable result for --json mode.
type dnsRecordResult struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl"`
	Proxied    bool   `json:"proxied"`
	Comment    string `json:"comment,omitempty"`
	CreatedOn  string `json:"created_on,omitempty"`
	ModifiedOn string `json:"modified_on,omitempty"`
}

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a DNS record in a zone",
	Long: `Create a new DNS record in the specified Cloudflare zone.

Supported record types: A, AAAA, CNAME, MX, TXT, NS

Examples:
  cf dns create --zone ZONE_ID --name example.com --type A --content 1.2.3.4
  cf dns create --zone ZONE_ID --name www --type CNAME --content example.com --proxied
  cf dns create --zone ZONE_ID --name example.com --type MX --content mail.example.com --ttl 3600
  cf dns create --zone ZONE_ID --name example.com --type TXT --content "v=spf1 include:example.com ~all"
  cf dns create --zone ZONE_ID --name example.com --type A --content 1.2.3.4 --json`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createZone, "zone", "", "Zone ID")
	createCmd.Flags().StringVar(&createDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	createCmd.Flags().StringVar(&createName, "name", "", "DNS record name (required)")
	createCmd.Flags().StringVar(&createType, "type", "", "Record type: A, AAAA, CNAME, MX, TXT, NS (required)")
	createCmd.Flags().StringVar(&createContent, "content", "", "Record content/value (required)")
	createCmd.Flags().IntVar(&createTTL, "ttl", 1, "TTL in seconds (1 = automatic)")
	createCmd.Flags().BoolVar(&createProxied, "proxied", false, "Enable Cloudflare proxy (orange cloud)")
	createCmd.Flags().StringVar(&createComment, "comment", "", "Optional comment for the record")

	createCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = createCmd.MarkFlagRequired("name")
	_ = createCmd.MarkFlagRequired("type")
	_ = createCmd.MarkFlagRequired("content")
}

func runCreate(cmd *cobra.Command, _ []string) error {
	jsonFlag, _ := cmd.Root().PersistentFlags().GetBool("json")
	noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
	quiet, _ := cmd.Root().PersistentFlags().GetBool("quiet")
	token, _ := cmd.Root().PersistentFlags().GetString("token")

	p := output.New(jsonFlag, quiet, noColor)

	if createZone == "" && createDomain == "" {
		err := fmt.Errorf("one of --zone or --domain is required")
		p.Error("%v", err)
		return err
	}

	cfClient, err := client.New(client.Config{Token: token})
	if err != nil {
		p.Error("%v", err)
		return err
	}

	zoneID, err := client.ResolveZoneID(cmd.Context(), cfClient, createZone, createDomain)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Creating %s record for %s in zone %s…", strings.ToUpper(createType), createName, zoneID)

	recordType := strings.ToUpper(createType)
	ttl := dns.TTL(createTTL)

	body, err := buildRecordBody(recordType, ttl)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	resp, apiErr := cfClient.DNS.Records.New(
		context.Background(),
		dns.RecordNewParams{
			ZoneID: cf.F(zoneID),
			Body:   body,
		},
	)
	if apiErr != nil {
		p.Error("API error: %v", apiErr)
		return apiErr
	}

	result := dnsRecordResult{
		ID:         resp.ID,
		Name:       resp.Name,
		Type:       string(resp.Type),
		Content:    resp.Content,
		TTL:        int(resp.TTL),
		Proxied:    resp.Proxied,
		Comment:    resp.Comment,
		CreatedOn:  resp.CreatedOn.String(),
		ModifiedOn: resp.ModifiedOn.String(),
	}

	if jsonFlag {
		p.PrintJSON(result)
		return nil
	}

	p.Success("DNS record created")
	p.KV([][2]string{
		{"ID", result.ID},
		{"Name", result.Name},
		{"Type", result.Type},
		{"Content", result.Content},
		{"TTL", ttlDisplay(result.TTL)},
		{"Proxied", fmt.Sprintf("%v", result.Proxied)},
		{"Comment", result.Comment},
		{"Created", result.CreatedOn},
	})
	return nil
}

// buildRecordBody constructs the correct typed record body for the v6 SDK.
func buildRecordBody(recordType string, ttl dns.TTL) (dns.RecordNewParamsBodyUnion, error) {
	switch recordType {
	case "A":
		p := dns.ARecordParam{
			Name:    cf.F(createName),
			Type:    cf.F(dns.ARecordTypeA),
			Content: cf.F(createContent),
			TTL:     cf.F(ttl),
			Proxied: cf.Bool(createProxied),
		}
		if createComment != "" {
			p.Comment = cf.F(createComment)
		}
		return p, nil
	case "AAAA":
		p := dns.AAAARecordParam{
			Name:    cf.F(createName),
			Type:    cf.F(dns.AAAARecordTypeAAAA),
			Content: cf.F(createContent),
			TTL:     cf.F(ttl),
			Proxied: cf.Bool(createProxied),
		}
		if createComment != "" {
			p.Comment = cf.F(createComment)
		}
		return p, nil
	case "CNAME":
		p := dns.CNAMERecordParam{
			Name:    cf.F(createName),
			Type:    cf.F(dns.CNAMERecordTypeCNAME),
			Content: cf.F(createContent),
			TTL:     cf.F(ttl),
			Proxied: cf.Bool(createProxied),
		}
		if createComment != "" {
			p.Comment = cf.F(createComment)
		}
		return p, nil
	case "MX":
		p := dns.MXRecordParam{
			Name:    cf.F(createName),
			Type:    cf.F(dns.MXRecordTypeMX),
			Content: cf.F(createContent),
			TTL:     cf.F(ttl),
		}
		if createComment != "" {
			p.Comment = cf.F(createComment)
		}
		return p, nil
	case "TXT":
		p := dns.TXTRecordParam{
			Name:    cf.F(createName),
			Type:    cf.F(dns.TXTRecordTypeTXT),
			Content: cf.F(createContent),
			TTL:     cf.F(ttl),
		}
		if createComment != "" {
			p.Comment = cf.F(createComment)
		}
		return p, nil
	case "NS":
		p := dns.NSRecordParam{
			Name:    cf.F(createName),
			Type:    cf.F(dns.NSRecordTypeNS),
			Content: cf.F(createContent),
			TTL:     cf.F(ttl),
		}
		if createComment != "" {
			p.Comment = cf.F(createComment)
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unsupported record type %q; supported: A, AAAA, CNAME, MX, TXT, NS", recordType)
	}
}

func ttlDisplay(ttl int) string {
	if ttl == 1 {
		return "auto"
	}
	return fmt.Sprintf("%ds", ttl)
}
