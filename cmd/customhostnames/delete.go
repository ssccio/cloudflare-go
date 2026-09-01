package customhostnames

import (
	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	deleteZone       string
	deleteDomain     string
	deleteHostnameID string
	deleteDryRun     bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a custom hostname by ID",
	Long: `Delete a custom hostname. The custom hostname ID is required — use
` + "`cf custom-hostnames list`" + ` or ` + "`cf custom-hostnames get`" + ` to find it.

Examples:
  cf custom-hostnames delete --domain example.com --hostname-id HOSTNAME_ID
  cf custom-hostnames delete --zone ZONE_ID --hostname-id HOSTNAME_ID --dry-run`,
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteZone, "zone", "", "Zone ID")
	deleteCmd.Flags().StringVar(&deleteDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	deleteCmd.Flags().StringVar(&deleteHostnameID, "hostname-id", "", "Custom hostname ID to delete (required)")
	deleteCmd.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Show what would be deleted without calling the API")

	deleteCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = deleteCmd.MarkFlagRequired("hostname-id")
}

func runDelete(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, deleteZone, deleteDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	if cmdutil.DryRun(p, deleteDryRun, "delete custom hostname %s from zone %s", deleteHostnameID, ctx.ZoneID) {
		return nil
	}

	p.Info("Deleting custom hostname %s from zone %s…", deleteHostnameID, ctx.ZoneID)

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
