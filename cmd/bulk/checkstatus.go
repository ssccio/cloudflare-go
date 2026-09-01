package bulk

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	checkZone   string
	checkDomain string
	checkFile   string
)

// hostnameStatus is the per-domain status report line.
type hostnameStatus struct {
	Hostname  string `json:"hostname"             toon:"hostname"`
	Found     bool   `json:"found"                toon:"found"`
	Status    string `json:"status"               toon:"status"`
	SSLStatus string `json:"ssl_status,omitempty" toon:"ssl_status,omitempty"`
	SSLMethod string `json:"ssl_method,omitempty" toon:"ssl_method,omitempty"`
	ID        string `json:"id,omitempty"         toon:"id,omitempty"`
}

// checkStatusResult is the full status report.
type checkStatusResult struct {
	Zone          string           `json:"zone"             toon:"zone"`
	File          string           `json:"file"             toon:"file"`
	Total         int              `json:"total"            toon:"total"`
	Found         int              `json:"found"            toon:"found"`
	NotFound      int              `json:"not_found"        toon:"not_found"`
	BySSLStatus   map[string]int   `json:"by_ssl_status"    toon:"by_ssl_status"`
	Hostnames     []hostnameStatus `json:"hostnames"        toon:"hostnames"`
	ZoneHostnames int              `json:"zone_hostnames"   toon:"zone_hostnames"`
}

var checkStatusCmd = &cobra.Command{
	Use:   "check-status",
	Short: "Check custom hostname status for every domain in a file",
	Long: `Check the custom hostname and SSL status of every domain listed in a file.

The file holds one hostname per line. Blank lines and lines starting with # are
skipped, entries are lowercased, and duplicates are removed.

All custom hostnames in the zone are fetched once and matched locally, so the
command makes one listing pass rather than one API call per domain.

Examples:
  cf bulk check-status --zone ZONE_ID --file hostnames.txt
  cf bulk check-status --domain example.com --file hostnames.txt
  cf bulk check-status --zone ZONE_ID --file hostnames.txt --json
  cf bulk check-status --zone ZONE_ID --file hostnames.txt --json --query "[?found==` + "`false`" + `].hostname"`,
	RunE: runCheckStatus,
}

func init() {
	checkStatusCmd.Flags().StringVar(&checkZone, "zone", "", "Zone ID")
	checkStatusCmd.Flags().StringVar(&checkDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	checkStatusCmd.Flags().StringVar(&checkFile, "file", "", "File of hostnames, one per line (required)")

	checkStatusCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = checkStatusCmd.MarkFlagRequired("file")
}

func runCheckStatus(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, checkZone, checkDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	wanted, err := readHostnameFile(checkFile)
	if err != nil {
		p.Error("%v", err)
		return err
	}

	p.Info("Read %d unique hostname(s) from %s", len(wanted), checkFile)
	p.Info("Fetching custom hostnames for zone %s…", ctx.ZoneID)

	records, err := listCustomHostnames(cmd.Context(), ctx.Client, ctx.ZoneID)
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	idx := hostnameIndex(records)

	result := checkStatusResult{
		Zone:          ctx.ZoneID,
		File:          checkFile,
		Total:         len(wanted),
		BySSLStatus:   map[string]int{},
		Hostnames:     make([]hostnameStatus, 0, len(wanted)),
		ZoneHostnames: len(records),
	}

	for _, h := range wanted {
		rec, ok := idx[h]
		if !ok {
			result.NotFound++
			result.Hostnames = append(result.Hostnames, hostnameStatus{
				Hostname: h,
				Found:    false,
				Status:   "not_found",
			})
			continue
		}
		result.Found++
		sslStatus := string(rec.SSL.Status)
		result.BySSLStatus[sslStatus]++
		result.Hostnames = append(result.Hostnames, hostnameStatus{
			Hostname:  h,
			Found:     true,
			Status:    string(rec.Status),
			SSLStatus: sslStatus,
			SSLMethod: string(rec.SSL.Method),
			ID:        rec.ID,
		})
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	sslKeys := make([]string, 0, len(result.BySSLStatus))
	for k := range result.BySSLStatus {
		sslKeys = append(sslKeys, k)
	}
	sort.Strings(sslKeys)

	pairs := [][2]string{
		{"Zone", result.Zone},
		{"Total checked", fmt.Sprintf("%d", result.Total)},
		{"Found", fmt.Sprintf("%d", result.Found)},
		{"Not found", fmt.Sprintf("%d", result.NotFound)},
	}
	for _, k := range sslKeys {
		label := k
		if label == "" {
			label = "(no ssl status)"
		}
		pairs = append(pairs, [2]string{"SSL " + label, fmt.Sprintf("%d", result.BySSLStatus[k])})
	}
	p.KV(pairs)

	rows := make([][]string, 0, len(result.Hostnames))
	for _, h := range result.Hostnames {
		rows = append(rows, []string{h.Hostname, h.Status, h.SSLStatus, h.SSLMethod})
	}
	p.Table([]string{"HOSTNAME", "STATUS", "SSL STATUS", "METHOD"}, rows)
	return nil
}
