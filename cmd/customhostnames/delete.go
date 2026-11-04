package customhostnames

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	deleteZone       string
	deleteDomain     string
	deleteHostname   string
	deleteHostnameID string
	deleteDryRun     bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a custom hostname by name or ID",
	Long: `Delete a custom hostname, looked up by name with --hostname or given
directly with --hostname-id.

Examples:
  cf custom-hostnames delete --domain example.com --hostname learn.customer.com --dry-run
  cf custom-hostnames delete --domain example.com --hostname-id HOSTNAME_ID
  cf custom-hostnames delete --zone ZONE_ID --hostname-id HOSTNAME_ID --dry-run`,
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteZone, "zone", "", "Zone ID")
	deleteCmd.Flags().StringVar(&deleteDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	deleteCmd.Flags().StringVar(&deleteHostname, "hostname", "", "Custom hostname to delete")
	deleteCmd.Flags().StringVar(&deleteHostnameID, "hostname-id", "", "Custom hostname ID to delete")
	deleteCmd.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Show what would be deleted without calling the API")

	deleteCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	deleteCmd.MarkFlagsMutuallyExclusive("hostname", "hostname-id")
	deleteCmd.MarkFlagsOneRequired("hostname", "hostname-id")
}

func runDelete(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, deleteZone, deleteDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	target := deleteHostnameID
	if deleteHostname != "" {
		p.Info("Looking up %s in zone %s…", deleteHostname, ctx.ZoneID)
		existing, err := findByHostname(cmd.Context(), ctx.Client, ctx.ZoneID, deleteHostname)
		if err != nil {
			p.Error("%v", err)
			return err
		}
		deleteHostnameID = existing.ID
		target = fmt.Sprintf("%s (%s)", existing.Hostname, existing.ID)
	}

	if cmdutil.DryRun(p, deleteDryRun, "delete custom hostname %s from zone %s", target, ctx.ZoneID) {
		return nil
	}

	p.Info("Deleting custom hostname %s from zone %s…", target, ctx.ZoneID)

	resp, err := ctx.Client.CustomHostnames.Delete(cmd.Context(), deleteHostnameID, custom_hostnames.CustomHostnameDeleteParams{
		ZoneID: cf.F(ctx.ZoneID),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	id := deleteHostnameID
	if resp != nil && resp.ID != "" {
		id = resp.ID
	}

	if p.JSON || p.TOON {
		p.PrintResult(map[string]any{"id": id, "deleted": true})
		return nil
	}

	p.Success("Custom hostname deleted: %s", id)
	return nil
}
