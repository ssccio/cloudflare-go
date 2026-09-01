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
	listZone   string
	listDomain string
	listStatus string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List custom hostnames in a zone",
	Long: `List all custom hostnames in a Cloudflare zone, optionally filtered by SSL status.

Examples:
  cf custom-hostnames list --zone ZONE_ID
  cf custom-hostnames list --domain example.com
  cf custom-hostnames list --domain example.com --status pending_validation
  cf custom-hostnames list --domain example.com --json
  cf custom-hostnames list --domain example.com --toon --query "[?ssl_status!='active'].hostname"`,
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVar(&listZone, "zone", "", "Zone ID")
	listCmd.Flags().StringVar(&listDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	listCmd.Flags().StringVar(&listStatus, "status", "", "Filter by SSL status (e.g. active, pending_validation, pending_issuance)")

	listCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runList(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, listZone, listDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	p.Info("Fetching custom hostnames for zone %s…", ctx.ZoneID)

	hostnames, err := listHostnames(cmd, ctx, listStatus)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	if p.JSON || p.TOON {
		p.PrintResult(hostnames)
		return nil
	}

	if len(hostnames) == 0 {
		p.Info("No custom hostnames found.")
		return nil
	}

	rows := make([][]string, 0, len(hostnames))
	for _, h := range hostnames {
		rows = append(rows, []string{
			h.Hostname,
			dash(h.Status),
			dash(h.SSLStatus),
			dash(h.SSLMethod),
			h.ID,
		})
	}

	p.Table(
		[]string{"HOSTNAME", "STATUS", "SSL STATUS", "SSL METHOD", "ID"},
		rows,
	)
	fmt.Fprintf(cmd.OutOrStdout(), "  %d custom hostname(s)\n", len(hostnames))
	return nil
}

// listHostnames fetches every custom hostname in the zone, applying the SSL
// status filter when one is given.
func listHostnames(cmd *cobra.Command, ctx *cmdutil.Context, sslStatus string) ([]customHostnameResult, error) {
	params := custom_hostnames.CustomHostnameListParams{
		ZoneID: cf.F(ctx.ZoneID),
	}

	var hostnames []customHostnameResult
	iter := ctx.Client.CustomHostnames.ListAutoPaging(cmd.Context(), params)
	for iter.Next() {
		h := fromListResponse(iter.Current())
		if sslStatus != "" && !strings.EqualFold(h.SSLStatus, sslStatus) {
			continue
		}
		hostnames = append(hostnames, h)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return hostnames, nil
}
