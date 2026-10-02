package logpush

import (
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/logpush"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	createZone        string
	createDomain      string
	createName        string
	createDataset     string
	createDestination string
	createFields      []string
	createFilter      string
	createChallenge   string
	createTimestamp   string
	createDisabled    bool
	createDryRun      bool
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a zone Logpush job",
	Long: `Create a zone-scoped Logpush job. Output is ndjson with the listed fields.

--ownership-challenge is the contents of the file "cf logpush ownership"
wrote to the destination. --filter takes Logpush filter JSON as-is.

Examples:
  cf logpush create --domain example.com --name http-to-s3 \
    --dataset http_requests \
    --destination 's3://bucket/prefix?region=us-east-1' \
    --fields EdgeStartTimestamp,RayID,ClientIP,BotScore \
    --ownership-challenge "$(aws s3 cp s3://bucket/prefix/<file> -)" --dry-run`,
	RunE: runCreate,
}

func init() {
	createCmd.Flags().StringVar(&createZone, "zone", "", "Zone ID")
	createCmd.Flags().StringVar(&createDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	createCmd.Flags().StringVar(&createName, "name", "", "Job name (letters, digits, dash, underscore, dot)")
	createCmd.Flags().StringVar(&createDataset, "dataset", "http_requests", "Dataset, e.g. http_requests, firewall_events")
	createCmd.Flags().StringVar(&createDestination, "destination", "", "Destination conf (required)")
	createCmd.Flags().StringSliceVar(&createFields, "fields", nil, "Fields to include, comma-separated or repeated (required)")
	createCmd.Flags().StringVar(&createFilter, "filter", "", "Logpush filter JSON")
	createCmd.Flags().StringVar(&createChallenge, "ownership-challenge", "", "Ownership challenge token (required for new destinations)")
	createCmd.Flags().StringVar(&createTimestamp, "timestamp-format", "rfc3339", "unixnano, unix or rfc3339")
	createCmd.Flags().BoolVar(&createDisabled, "disabled", false, "Create the job disabled")
	createCmd.Flags().BoolVar(&createDryRun, "dry-run", false, "Show the job without creating it")
	createCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = createCmd.MarkFlagRequired("destination")
	_ = createCmd.MarkFlagRequired("fields")
}

func runCreate(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, createZone, createDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	if cmdutil.DryRun(p, createDryRun, "create %s job %q on zone %s → %s, enabled=%v, %d fields: %s",
		createDataset, createName, c.ZoneID, createDestination, !createDisabled, len(createFields), strings.Join(createFields, ",")) {
		return nil
	}

	params := logpush.JobNewParams{
		ZoneID:          cf.F(c.ZoneID),
		Dataset:         cf.F(logpush.JobNewParamsDataset(createDataset)),
		DestinationConf: cf.F(createDestination),
		Enabled:         cf.F(!createDisabled),
		OutputOptions: cf.F(logpush.OutputOptionsParam{
			FieldNames:      cf.F(createFields),
			OutputType:      cf.F(logpush.OutputOptionsOutputTypeNdjson),
			TimestampFormat: cf.F(logpush.OutputOptionsTimestampFormat(createTimestamp)),
		}),
	}
	if createName != "" {
		params.Name = cf.F(createName)
	}
	if createFilter != "" {
		params.Filter = cf.F(createFilter)
	}
	if createChallenge != "" {
		params.OwnershipChallenge = cf.F(strings.TrimSpace(createChallenge))
	}

	job, err := c.Client.Logpush.Jobs.New(cmd.Context(), params)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	row := jobRow(*job)
	if p.JSON || p.TOON {
		p.PrintResult(row)
		return nil
	}
	p.Info("Created Logpush job %d.", row.ID)
	return nil
}
