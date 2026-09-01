package bulk

import (
	"fmt"
	"time"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/output"
)

var (
	importZone      string
	importDomain    string
	importFile      string
	importSSLMethod string
	importBatchSize int
	importDelayMS   int
	importDryRun    bool
)

// importedHostname is one line of the import report.
type importedHostname struct {
	Hostname string `json:"hostname"          toon:"hostname"`
	Action   string `json:"action"            toon:"action"`
	ID       string `json:"id,omitempty"      toon:"id,omitempty"`
	Status   string `json:"status,omitempty"  toon:"status,omitempty"`
	Error    string `json:"error,omitempty"   toon:"error,omitempty"`
}

// importResult is the full result of an import run.
type importResult struct {
	Zone      string             `json:"zone"       toon:"zone"`
	File      string             `json:"file"       toon:"file"`
	SSLMethod string             `json:"ssl_method" toon:"ssl_method"`
	DryRun    bool               `json:"dry_run"    toon:"dry_run"`
	Total     int                `json:"total"      toon:"total"`
	Created   int                `json:"created"    toon:"created"`
	Existing  int                `json:"existing"   toon:"existing"`
	Failed    int                `json:"failed"     toon:"failed"`
	Hostnames []importedHostname `json:"hostnames"  toon:"hostnames"`
}

var importHostnamesCmd = &cobra.Command{
	Use:   "import-hostnames",
	Short: "Create custom hostnames in bulk from a file",
	Long: `Create Cloudflare for SaaS custom hostnames in bulk from a file.

The file holds one hostname per line. Blank lines and lines starting with # are
skipped, entries are lowercased, and duplicates are removed.

Existing custom hostnames are listed first and skipped, so the import is
idempotent. Remaining hostnames are created in batches, sleeping between batches.
A failure on one hostname does not abort the run; failures are collected and
reported, and the command exits non-zero if any hostname failed.

Examples:
  cf bulk import-hostnames --zone ZONE_ID --file hostnames.txt --dry-run
  cf bulk import-hostnames --domain example.com --file hostnames.txt
  cf bulk import-hostnames --zone ZONE_ID --file hostnames.txt --ssl-method txt
  cf bulk import-hostnames --zone ZONE_ID --file hostnames.txt --batch-size 10 --delay-ms 500
  cf bulk import-hostnames --zone ZONE_ID --file hostnames.txt --json`,
	RunE: runImportHostnames,
}

func init() {
	importHostnamesCmd.Flags().StringVar(&importZone, "zone", "", "Zone ID")
	importHostnamesCmd.Flags().StringVar(&importDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	importHostnamesCmd.Flags().StringVar(&importFile, "file", "", "File of hostnames, one per line (required)")
	importHostnamesCmd.Flags().StringVar(&importSSLMethod, "ssl-method", "http", "DCV method: http, txt, cname")
	importHostnamesCmd.Flags().IntVar(&importBatchSize, "batch-size", 5, "Number of hostnames to create per batch")
	importHostnamesCmd.Flags().IntVar(&importDelayMS, "delay-ms", 200, "Milliseconds to sleep between batches")
	importHostnamesCmd.Flags().BoolVar(&importDryRun, "dry-run", false, "Show what would be created without calling the API")

	importHostnamesCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = importHostnamesCmd.MarkFlagRequired("file")
}

// resolveDCVMethod validates the --ssl-method value.
func resolveDCVMethod(v string) (custom_hostnames.DCVMethod, error) {
	switch v {
	case "http", "txt", "email", "cname":
		return custom_hostnames.DCVMethod(v), nil
	default:
		return "", fmt.Errorf("unsupported --ssl-method %q; supported: http, txt, cname", v)
	}
}

func runImportHostnames(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, importZone, importDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	if importBatchSize < 1 {
		err := fmt.Errorf("--batch-size must be at least 1")
		p.Error("%v", err)
		return err
	}
	if importDelayMS < 0 {
		err := fmt.Errorf("--delay-ms cannot be negative")
		p.Error("%v", err)
		return err
	}

	method, err := resolveDCVMethod(importSSLMethod)
	if err != nil {
		p.Error("%v", err)
		return err
	}
	if method == "cname" {
		p.Notice("Note: the Cloudflare API accepts http, txt, and email DCV methods; \"cname\" may be rejected per hostname.")
	}

	wanted, err := readHostnameFile(importFile)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Read %d unique hostname(s) from %s", len(wanted), importFile)
	p.Info("Listing existing custom hostnames in zone %s…", ctx.ZoneID)

	existingRecords, err := listCustomHostnames(cmd.Context(), ctx.Client, ctx.ZoneID)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	existing := hostnameIndex(existingRecords)

	result := importResult{
		Zone:      ctx.ZoneID,
		File:      importFile,
		SSLMethod: string(method),
		DryRun:    importDryRun,
		Total:     len(wanted),
		Hostnames: make([]importedHostname, 0, len(wanted)),
	}

	var toCreate []string
	for _, h := range wanted {
		if rec, ok := existing[h]; ok {
			result.Existing++
			result.Hostnames = append(result.Hostnames, importedHostname{
				Hostname: h,
				Action:   "exists",
				ID:       rec.ID,
				Status:   string(rec.Status),
			})
			continue
		}
		toCreate = append(toCreate, h)
	}

	if importDryRun {
		for _, h := range toCreate {
			result.Hostnames = append(result.Hostnames, importedHostname{
				Hostname: h,
				Action:   "would_create",
			})
		}
		batches := (len(toCreate) + importBatchSize - 1) / importBatchSize
		cmdutil.DryRun(p, true,
			"create %d custom hostname(s) in zone %s using DCV method %q, in %d batch(es) of %d with %dms between batches; %d already exist and would be skipped",
			len(toCreate), ctx.ZoneID, method, batches, importBatchSize, importDelayMS, result.Existing)
		for _, h := range toCreate {
			p.Notice("[DRY RUN]   would create %s", h)
		}
		if p.JSON || p.TOON {
			p.PrintResult(result)
			return nil
		}
		printImportSummary(p, result)
		return nil
	}

	delay := time.Duration(importDelayMS) * time.Millisecond
	for start := 0; start < len(toCreate); start += importBatchSize {
		end := start + importBatchSize
		if end > len(toCreate) {
			end = len(toCreate)
		}
		if start > 0 && delay > 0 {
			time.Sleep(delay)
		}

		batch := toCreate[start:end]
		p.Info("Creating hostnames %d-%d of %d…", start+1, end, len(toCreate))

		for _, h := range batch {
			resp, apiErr := ctx.Client.CustomHostnames.New(cmd.Context(), custom_hostnames.CustomHostnameNewParams{
				ZoneID:   cf.F(ctx.ZoneID),
				Hostname: cf.F(h),
				SSL: cf.F(custom_hostnames.CustomHostnameNewParamsSSL{
					Method: cf.F(method),
					Type:   cf.F(custom_hostnames.DomainValidationTypeDv),
				}),
			})
			if apiErr != nil {
				result.Failed++
				result.Hostnames = append(result.Hostnames, importedHostname{
					Hostname: h,
					Action:   "failed",
					Error:    apiErr.Error(),
				})
				p.Error("%s: %v", h, apiErr)
				continue
			}
			result.Created++
			result.Hostnames = append(result.Hostnames, importedHostname{
				Hostname: h,
				Action:   "created",
				ID:       resp.ID,
				Status:   string(resp.Status),
			})
		}
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		if result.Failed > 0 {
			return fmt.Errorf("%d hostname(s) failed to import", result.Failed)
		}
		return nil
	}

	printImportSummary(p, result)
	if result.Failed > 0 {
		return fmt.Errorf("%d hostname(s) failed to import", result.Failed)
	}
	return nil
}

func printImportSummary(p *output.Printer, result importResult) {
	label := "Import complete"
	if result.DryRun {
		label = "Dry run complete"
	}
	p.Success("%s", label)
	p.KV([][2]string{
		{"Zone", result.Zone},
		{"File", result.File},
		{"SSL method", result.SSLMethod},
		{"Total in file", fmt.Sprintf("%d", result.Total)},
		{"Created", fmt.Sprintf("%d", result.Created)},
		{"Already existed", fmt.Sprintf("%d", result.Existing)},
		{"Failed", fmt.Sprintf("%d", result.Failed)},
	})

	rows := make([][]string, 0, len(result.Hostnames))
	for _, h := range result.Hostnames {
		detail := h.Status
		if h.Error != "" {
			detail = h.Error
		}
		rows = append(rows, []string{h.Hostname, h.Action, h.ID, detail})
	}
	if len(rows) > 0 {
		p.Table([]string{"HOSTNAME", "ACTION", "ID", "DETAIL"}, rows)
	}
}
