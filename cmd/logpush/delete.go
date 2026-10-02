package logpush

import (
	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/logpush"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	deleteZone   string
	deleteDomain string
	deleteJobID  int64
	deleteDryRun bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete a zone Logpush job",
	Long: `Delete a Logpush job by ID. Objects already pushed to the destination stay.

Examples:
  cf logpush delete --domain example.com --job-id 123456 --dry-run`,
	RunE: runDelete,
}

func init() {
	deleteCmd.Flags().StringVar(&deleteZone, "zone", "", "Zone ID")
	deleteCmd.Flags().StringVar(&deleteDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	deleteCmd.Flags().Int64Var(&deleteJobID, "job-id", 0, "Logpush job ID (required)")
	deleteCmd.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Show what would be deleted without deleting")
	deleteCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = deleteCmd.MarkFlagRequired("job-id")
}

func runDelete(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, deleteZone, deleteDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	job, err := c.Client.Logpush.Jobs.Get(cmd.Context(), deleteJobID, logpush.JobGetParams{ZoneID: cf.F(c.ZoneID)})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	if cmdutil.DryRun(p, deleteDryRun, "delete Logpush job %d %q (%s → %s) on zone %s", job.ID, job.Name, job.Dataset, job.DestinationConf, c.ZoneID) {
		return nil
	}
	if _, err := c.Client.Logpush.Jobs.Delete(cmd.Context(), deleteJobID, logpush.JobDeleteParams{ZoneID: cf.F(c.ZoneID)}); err != nil {
		p.Error("API error: %v", err)
		return err
	}
	p.Info("Deleted Logpush job %d.", deleteJobID)
	return nil
}
