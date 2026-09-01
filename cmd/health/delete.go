package health

import (
	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/healthchecks"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	deleteZone   string
	deleteDomain string
	deleteID     string
	deleteDryRun bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a health check from a zone",
	Long: `Delete a health check by ID. This is irreversible.

Examples:
  cf health delete --zone ZONE_ID --id HEALTHCHECK_ID
  cf health delete --domain example.com --id HEALTHCHECK_ID
  cf health delete --domain example.com --id HEALTHCHECK_ID --dry-run
  cf health delete --domain example.com --id HEALTHCHECK_ID --json`,
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteZone, "zone", "", "Zone ID")
	deleteCmd.Flags().StringVar(&deleteDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	deleteCmd.Flags().StringVar(&deleteID, "id", "", "Health check ID to delete (required)")
	deleteCmd.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Show what would be deleted without making the API call")

	deleteCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = deleteCmd.MarkFlagRequired("id")
}

func runDelete(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, deleteZone, deleteDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	// Fetch the check so the dry-run and real paths can both name what's being deleted.
	p.Info("Fetching health check %s…", deleteID)
	check, err := c.Client.Healthchecks.Get(
		cmd.Context(),
		deleteID,
		healthchecks.HealthcheckGetParams{ZoneID: cf.F(c.ZoneID)},
	)
	if err != nil {
		p.Error("health check not found: %v", err)
		return err
	}

	if cmdutil.DryRun(p, deleteDryRun, "delete health check %q (%s, id %s)", check.Name, check.Address, check.ID) {
		return nil
	}

	_, err = c.Client.Healthchecks.Delete(
		cmd.Context(),
		deleteID,
		healthchecks.HealthcheckDeleteParams{ZoneID: cf.F(c.ZoneID)},
	)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := map[string]string{"id": check.ID, "deleted": "true"}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Health check deleted: %s (%s)", check.Name, check.Address)
	return nil
}
