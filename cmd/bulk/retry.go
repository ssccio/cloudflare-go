package bulk

import (
	"fmt"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"
	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/output"
)

// defaultRetryStatuses is the SSL status set retry-failed selects by default.
var defaultRetryStatuses = []string{
	"pending_validation",
	"validation_timed_out",
	"issuance_timed_out",
	"pending_issuance",
	"pending_deployment",
}

var (
	retryZone     string
	retryDomain   string
	retryStatuses []string
	retryDryRun   bool
)

// retriedHostname records one retry attempt.
type retriedHostname struct {
	Hostname       string `json:"hostname"           toon:"hostname"`
	ID             string `json:"id"                 toon:"id"`
	PreviousStatus string `json:"previous_status"    toon:"previous_status"`
	Method         string `json:"method"             toon:"method"`
	Action         string `json:"action"             toon:"action"`
	Error          string `json:"error,omitempty"    toon:"error,omitempty"`
}

// retryResult is the full retry report.
type retryResult struct {
	Zone      string            `json:"zone"      toon:"zone"`
	DryRun    bool              `json:"dry_run"   toon:"dry_run"`
	Statuses  []string          `json:"statuses"  toon:"statuses"`
	Total     int               `json:"total"     toon:"total"`
	Selected  int               `json:"selected"  toon:"selected"`
	Retried   int               `json:"retried"   toon:"retried"`
	Errors    int               `json:"errors"    toon:"errors"`
	Hostnames []retriedHostname `json:"hostnames" toon:"hostnames"`
}

var retryFailedCmd = &cobra.Command{
	Use:   "retry-failed",
	Short: "Re-trigger DCV validation on custom hostnames stuck in a failed SSL state",
	Long: `Re-trigger domain control validation on every custom hostname whose SSL status
is one of the selected statuses.

By default the selected statuses are pending_validation, validation_timed_out,
issuance_timed_out, pending_issuance, and pending_deployment. Use --status
(repeatable) to override that set.

Each hostname is edited with its existing DCV method, or http when the existing
method is unknown. A failure on one hostname does not abort the run.

Examples:
  cf bulk retry-failed --zone ZONE_ID --dry-run
  cf bulk retry-failed --domain example.com
  cf bulk retry-failed --zone ZONE_ID --status validation_timed_out
  cf bulk retry-failed --zone ZONE_ID --json`,
	RunE: runRetryFailed,
}

func init() {
	retryFailedCmd.Flags().StringVar(&retryZone, "zone", "", "Zone ID")
	retryFailedCmd.Flags().StringVar(&retryDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	retryFailedCmd.Flags().StringArrayVar(&retryStatuses, "status", nil,
		"SSL status to select; repeatable (default pending_validation, validation_timed_out, issuance_timed_out, pending_issuance, pending_deployment)")
	retryFailedCmd.Flags().BoolVar(&retryDryRun, "dry-run", false, "Show what would be retried without calling the API")

	retryFailedCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runRetryFailed(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, retryZone, retryDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	statuses := retryStatuses
	if len(statuses) == 0 {
		statuses = defaultRetryStatuses
	}
	selected := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		selected[strings.ToLower(strings.TrimSpace(s))] = true
	}

	p.Info("Fetching custom hostnames for zone %s…", ctx.ZoneID)

	records, err := listCustomHostnames(cmd.Context(), ctx.Client, ctx.ZoneID)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := retryResult{
		Zone:     ctx.ZoneID,
		DryRun:   retryDryRun,
		Statuses: statuses,
		Total:    len(records),
	}

	type candidate struct {
		id       string
		hostname string
		status   string
		method   custom_hostnames.DCVMethod
	}
	var candidates []candidate
	for _, r := range records {
		status := string(r.SSL.Status)
		if !selected[status] {
			continue
		}
		method := r.SSL.Method
		if method == "" {
			method = custom_hostnames.DCVMethodHTTP
		}
		candidates = append(candidates, candidate{
			id:       r.ID,
			hostname: strings.ToLower(r.Hostname),
			status:   status,
			method:   method,
		})
	}
	result.Selected = len(candidates)

	if retryDryRun {
		for _, c := range candidates {
			result.Hostnames = append(result.Hostnames, retriedHostname{
				Hostname:       c.hostname,
				ID:             c.id,
				PreviousStatus: c.status,
				Method:         string(c.method),
				Action:         "would_retry",
			})
		}
		cmdutil.DryRun(p, true,
			"re-trigger validation on %d of %d custom hostname(s) in zone %s with SSL status in [%s]",
			len(candidates), len(records), ctx.ZoneID, strings.Join(statuses, ", "))
		for _, c := range candidates {
			p.Notice("[DRY RUN]   would retry %s (id %s, status %s, method %s)", c.hostname, c.id, c.status, c.method)
		}
		if p.JSON || p.TOON {
			p.PrintResult(result)
			return nil
		}
		printRetrySummary(p, result)
		return nil
	}

	for _, c := range candidates {
		_, apiErr := ctx.Client.CustomHostnames.Edit(cmd.Context(), c.id, custom_hostnames.CustomHostnameEditParams{
			ZoneID: cf.F(ctx.ZoneID),
			SSL: cf.F(custom_hostnames.CustomHostnameEditParamsSSL{
				Method: cf.F(c.method),
				Type:   cf.F(custom_hostnames.DomainValidationTypeDv),
			}),
		})
		entry := retriedHostname{
			Hostname:       c.hostname,
			ID:             c.id,
			PreviousStatus: c.status,
			Method:         string(c.method),
			Action:         "retried",
		}
		if apiErr != nil {
			result.Errors++
			entry.Action = "failed"
			entry.Error = apiErr.Error()
			p.Error("%s: %v", c.hostname, apiErr)
		} else {
			result.Retried++
		}
		result.Hostnames = append(result.Hostnames, entry)
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		if result.Errors > 0 {
			return fmt.Errorf("%d hostname(s) failed to retry", result.Errors)
		}
		return nil
	}

	printRetrySummary(p, result)
	if result.Errors > 0 {
		return fmt.Errorf("%d hostname(s) failed to retry", result.Errors)
	}
	return nil
}

func printRetrySummary(p *output.Printer, result retryResult) {
	label := "Retry complete"
	if result.DryRun {
		label = "Dry run complete"
	}
	p.Success("%s", label)
	p.KV([][2]string{
		{"Zone", result.Zone},
		{"Statuses", strings.Join(result.Statuses, ", ")},
		{"Hostnames in zone", fmt.Sprintf("%d", result.Total)},
		{"Selected", fmt.Sprintf("%d", result.Selected)},
		{"Retried", fmt.Sprintf("%d", result.Retried)},
		{"Errors", fmt.Sprintf("%d", result.Errors)},
	})

	rows := make([][]string, 0, len(result.Hostnames))
	for _, h := range result.Hostnames {
		detail := h.Error
		rows = append(rows, []string{h.Hostname, h.PreviousStatus, h.Method, h.Action, detail})
	}
	if len(rows) > 0 {
		p.Table([]string{"HOSTNAME", "PREVIOUS STATUS", "METHOD", "ACTION", "ERROR"}, rows)
	}
}
