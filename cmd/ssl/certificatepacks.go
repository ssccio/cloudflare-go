package ssl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/ssl"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	certificatePacksZone   string
	certificatePacksDomain string
)

// maxHostsInTable is the number of hosts shown in the human-readable table
// before truncating with an ellipsis. The full list is always in --json.
const maxHostsInTable = 3

var certificatePacksCmd = &cobra.Command{
	Use:   "certificate-packs",
	Short: "List certificate packs for a zone",
	Long: `List all certificate packs for a Cloudflare zone.

Examples:
  cf ssl certificate-packs --zone ZONE_ID
  cf ssl certificate-packs --domain example.com --json`,
	RunE: runCertificatePacks,
}

func init() {
	certificatePacksCmd.Flags().StringVar(&certificatePacksZone, "zone", "", "Zone ID")
	certificatePacksCmd.Flags().StringVar(&certificatePacksDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	certificatePacksCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runCertificatePacks(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, certificatePacksZone, certificatePacksDomain)
	if err != nil {
		return err
	}
	p := c.Printer
	ctx := cmd.Context()

	p.Info("Fetching certificate packs for zone %s…", c.ZoneID)

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
		p.Error("API error: %v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(packs)
		return nil
	}

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
			hostsPreview(pk.Hosts),
		})
	}

	p.Table(
		[]string{"ID", "TYPE", "STATUS", "VALIDITY DAYS", "HOSTS"},
		rows,
	)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d certificate pack(s)\n", len(packs))
	return nil
}

// hostsPreview renders the first few hosts for table display, truncating
// with a count of the remainder. The full list is always available in --json.
func hostsPreview(hosts []string) string {
	if len(hosts) <= maxHostsInTable {
		return strings.Join(hosts, ", ")
	}
	return fmt.Sprintf("%s, … (+%d more)", strings.Join(hosts[:maxHostsInTable], ", "), len(hosts)-maxHostsInTable)
}
