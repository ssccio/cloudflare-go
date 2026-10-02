package logpush

import (
	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/logpush"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	ownershipZone        string
	ownershipDomain      string
	ownershipDestination string
	ownershipDryRun      bool
)

var ownershipCmd = &cobra.Command{
	Use:   "ownership",
	Short: "Ask Cloudflare to write an ownership challenge file to a destination",
	Long: `Request a Logpush ownership challenge. Cloudflare writes a file to the
destination; its contents are the token "cf logpush create" needs. The
command prints the file name it was written to. The file can be deleted
once the job exists.

Examples:
  cf logpush ownership --domain example.com \
    --destination 's3://bucket/prefix?region=us-east-1'`,
	RunE: runOwnership,
}

func init() {
	ownershipCmd.Flags().StringVar(&ownershipZone, "zone", "", "Zone ID")
	ownershipCmd.Flags().StringVar(&ownershipDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	ownershipCmd.Flags().StringVar(&ownershipDestination, "destination", "", "Destination conf, e.g. s3://bucket/prefix?region=us-east-1 (required)")
	ownershipCmd.Flags().BoolVar(&ownershipDryRun, "dry-run", false, "Show what would be requested without calling the API")
	ownershipCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = ownershipCmd.MarkFlagRequired("destination")
}

func runOwnership(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, ownershipZone, ownershipDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	if cmdutil.DryRun(p, ownershipDryRun, "request an ownership challenge for %s on zone %s", ownershipDestination, c.ZoneID) {
		return nil
	}
	res, err := c.Client.Logpush.Ownership.New(cmd.Context(), logpush.OwnershipNewParams{
		ZoneID:          cf.F(c.ZoneID),
		DestinationConf: cf.F(ownershipDestination),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	if p.JSON || p.TOON {
		p.PrintResult(map[string]any{"filename": res.Filename, "message": res.Message, "valid": res.Valid})
		return nil
	}
	p.KV([][2]string{{"filename", res.Filename}, {"message", res.Message}})
	return nil
}
