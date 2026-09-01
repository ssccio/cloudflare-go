package customhostnames

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	summaryZone   string
	summaryDomain string
)

// statusSummaryResult is the serialisable result for --json / --toon output.
type statusSummaryResult struct {
	Total            int            `json:"total"            toon:"total"`
	HostnameStatuses map[string]int `json:"hostname_statuses" toon:"hostname_statuses"`
	SSLStatuses      map[string]int `json:"ssl_statuses"      toon:"ssl_statuses"`
}

var statusSummaryCmd = &cobra.Command{
	Use:   "status-summary",
	Short: "Count custom hostnames by activation and SSL status",
	Long: `Summarise every custom hostname in a zone, grouped by hostname activation
status and by SSL certificate status.

Examples:
  cf custom-hostnames status-summary --domain example.com
  cf custom-hostnames status-summary --domain example.com --json`,
	RunE: runStatusSummary,
}

func init() {
	statusSummaryCmd.Flags().StringVar(&summaryZone, "zone", "", "Zone ID")
	statusSummaryCmd.Flags().StringVar(&summaryDomain, "domain", "", "Domain name (resolved to zone ID automatically)")

	statusSummaryCmd.MarkFlagsMutuallyExclusive("zone", "domain")
}

func runStatusSummary(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, summaryZone, summaryDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	p.Info("Fetching custom hostnames for zone %s…", ctx.ZoneID)

	hostnames, err := listHostnames(cmd, ctx, "")
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := statusSummaryResult{
		Total:            len(hostnames),
		HostnameStatuses: map[string]int{},
		SSLStatuses:      map[string]int{},
	}
	for _, h := range hostnames {
		result.HostnameStatuses[statusKey(h.Status)]++
		result.SSLStatuses[statusKey(h.SSLStatus)]++
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Total custom hostnames: %d\n", result.Total)
	fmt.Fprintf(out, "\nHostname status:\n")
	printCounts(cmd, result.HostnameStatuses)
	fmt.Fprintf(out, "\nSSL status:\n")
	printCounts(cmd, result.SSLStatuses)
	return nil
}

func statusKey(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func printCounts(cmd *cobra.Command, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(cmd.OutOrStdout(), "  %-24s %d\n", k, counts[k])
	}
}
