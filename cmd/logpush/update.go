package logpush

import (
	"fmt"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/logpush"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	updateZone      string
	updateDomain    string
	updateJob       int64
	updateAddFields []string
	updateDryRun    bool
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Add fields to a zone Logpush job",
	Long: `Add fields to an existing zone Logpush job. The command reads the job,
appends the new fields after the current ones, and sends the job's
output_options back with only field_names changed. Destination, sample
rate, timestamp format and the other output options are kept.

Every field is checked against the dataset's field list before any write,
so --dry-run also catches a field the dataset no longer has. Fields the job
already pushes are skipped.

Examples:
  cf logpush update --domain example.com --job 123456 \
    --add-fields JA4,BotScore,EdgeResponseBodyBytes --dry-run`,
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().StringVar(&updateZone, "zone", "", "Zone ID")
	updateCmd.Flags().StringVar(&updateDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	updateCmd.Flags().Int64Var(&updateJob, "job", 0, "Logpush job ID (required)")
	updateCmd.Flags().StringSliceVar(&updateAddFields, "add-fields", nil, "Fields to add, comma-separated or repeated (required)")
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "Show the change without making it")
	updateCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = updateCmd.MarkFlagRequired("job")
	_ = updateCmd.MarkFlagRequired("add-fields")
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	c, err := cmdutil.Zone(cmd, updateZone, updateDomain)
	if err != nil {
		return err
	}
	p := c.Printer
	ctx := cmd.Context()

	job, err := c.Client.Logpush.Jobs.Get(ctx, updateJob, logpush.JobGetParams{ZoneID: cf.F(c.ZoneID)})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	if job.LogpullOptions != "" {
		err := fmt.Errorf("job %d uses legacy logpull_options; update it to output_options first", job.ID)
		p.Error("%v", err)
		return err
	}

	known, err := datasetFields(cmd, c, string(job.Dataset))
	if err != nil {
		p.Error("API error reading %s fields: %v", job.Dataset, err)
		return err
	}
	have := map[string]bool{}
	for _, f := range job.OutputOptions.FieldNames {
		have[f] = true
	}
	var added, skipped, unknown []string
	for _, f := range updateAddFields {
		f = strings.TrimSpace(f)
		switch {
		case f == "":
		case !known[f]:
			unknown = append(unknown, f)
		case have[f]:
			skipped = append(skipped, f)
		default:
			have[f] = true
			added = append(added, f)
		}
	}
	if len(unknown) > 0 {
		err := fmt.Errorf("not in dataset %s: %s", job.Dataset, strings.Join(unknown, ","))
		p.Error("%v", err)
		return err
	}
	if len(skipped) > 0 {
		p.Notice("Already in job, skipped: %s", strings.Join(skipped, ","))
	}
	if len(added) == 0 {
		p.Info("Nothing to add to job %d.", job.ID)
		return nil
	}
	fields := append(append([]string{}, job.OutputOptions.FieldNames...), added...)

	o := job.OutputOptions
	if cmdutil.DryRun(p, updateDryRun,
		"update %s job %d %q on zone %s → %s: %d → %d fields, add %s (sample_rate=%v, timestamp_format=%q, output_type=%q kept)",
		job.Dataset, job.ID, job.Name, c.ZoneID, cmdutil.RedactDestination(job.DestinationConf),
		len(o.FieldNames), len(fields), strings.Join(added, ","), o.SampleRate, o.TimestampFormat, o.OutputType) {
		return nil
	}

	updated, err := c.Client.Logpush.Jobs.Update(ctx, job.ID, logpush.JobUpdateParams{
		ZoneID:        cf.F(c.ZoneID),
		OutputOptions: cf.F(outputOptionsParam(o, fields)),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	row := jobRow(*updated)
	if p.JSON || p.TOON {
		p.PrintResult(row)
		return nil
	}
	p.Info("Updated Logpush job %d: %d fields.", row.ID, len(row.Fields))
	return nil
}

// outputOptionsParam copies the job's current output options into a request
// with a new field list. Unset values stay unset so the API keeps its defaults.
func outputOptionsParam(o logpush.OutputOptions, fields []string) logpush.OutputOptionsParam {
	r := logpush.OutputOptionsParam{FieldNames: cf.F(fields)}
	if o.OutputType != "" {
		r.OutputType = cf.F(o.OutputType)
	}
	if o.TimestampFormat != "" {
		r.TimestampFormat = cf.F(o.TimestampFormat)
	}
	if o.SampleRate != 0 {
		r.SampleRate = cf.F(o.SampleRate)
	}
	if o.Cve2021_44228 {
		r.Cve2021_44228 = cf.F(true)
	}
	if o.BatchPrefix != "" {
		r.BatchPrefix = cf.F(o.BatchPrefix)
	}
	if o.BatchSuffix != "" {
		r.BatchSuffix = cf.F(o.BatchSuffix)
	}
	if o.FieldDelimiter != "" {
		r.FieldDelimiter = cf.F(o.FieldDelimiter)
	}
	if o.RecordDelimiter != "" {
		r.RecordDelimiter = cf.F(o.RecordDelimiter)
	}
	if o.RecordPrefix != "" {
		r.RecordPrefix = cf.F(o.RecordPrefix)
	}
	if o.RecordSuffix != "" {
		r.RecordSuffix = cf.F(o.RecordSuffix)
	}
	if o.RecordTemplate != "" {
		r.RecordTemplate = cf.F(o.RecordTemplate)
	}
	return r
}

// datasetFields returns the set of field names the zone dataset accepts.
func datasetFields(cmd *cobra.Command, c *cmdutil.Context, dataset string) (map[string]bool, error) {
	res, err := c.Client.Logpush.Datasets.Fields.Get(cmd.Context(),
		logpush.DatasetFieldGetParamsDatasetID(dataset), logpush.DatasetFieldGetParams{ZoneID: cf.F(c.ZoneID)})
	if err != nil {
		return nil, err
	}
	m, ok := (*res).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected field list type %T", *res)
	}
	known := make(map[string]bool, len(m))
	for k := range m {
		known[k] = true
	}
	return known, nil
}
