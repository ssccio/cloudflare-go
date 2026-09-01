// Package bulk implements the `cf bulk` subcommand group: bulk operations over
// Cloudflare for SaaS custom hostnames.
package bulk

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"
	"github.com/spf13/cobra"
)

// Cmd is the `cf bulk` parent command.
var Cmd = &cobra.Command{
	Use:   "bulk",
	Short: "Bulk operations over Cloudflare for SaaS custom hostnames",
	Long: `Bulk import, status-check, export, and validation-retry for Cloudflare for
SaaS custom hostnames.

These commands act on many hostnames at once. Mutating commands accept --dry-run,
which reports exactly what would change without making any API call that writes.`,
}

func init() {
	Cmd.AddCommand(importHostnamesCmd)
	Cmd.AddCommand(checkStatusCmd)
	Cmd.AddCommand(exportHostnamesCmd)
	Cmd.AddCommand(retryFailedCmd)
}

// readHostnameFile reads a hostname list: one hostname per line, blank lines and
// lines starting with # skipped, each entry lowercased, duplicates removed.
// Order of first appearance is preserved.
func readHostnameFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading hostname file: %w", err)
	}
	defer f.Close()

	var hostnames []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		h := strings.ToLower(line)
		if seen[h] {
			continue
		}
		seen[h] = true
		hostnames = append(hostnames, h)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading hostname file: %w", err)
	}
	if len(hostnames) == 0 {
		return nil, fmt.Errorf("no hostnames found in %s", path)
	}
	return hostnames, nil
}

// listCustomHostnames fetches every custom hostname in the zone.
func listCustomHostnames(ctx context.Context, c *cf.Client, zoneID string) ([]custom_hostnames.CustomHostnameListResponse, error) {
	var all []custom_hostnames.CustomHostnameListResponse
	iter := c.CustomHostnames.ListAutoPaging(ctx, custom_hostnames.CustomHostnameListParams{
		ZoneID:  cf.F(zoneID),
		PerPage: cf.F(50.0),
	})
	for iter.Next() {
		all = append(all, iter.Current())
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return all, nil
}

// hostnameIndex builds a lowercase hostname -> record lookup so callers can match
// by dict lookup instead of one API call per domain.
func hostnameIndex(records []custom_hostnames.CustomHostnameListResponse) map[string]custom_hostnames.CustomHostnameListResponse {
	idx := make(map[string]custom_hostnames.CustomHostnameListResponse, len(records))
	for _, r := range records {
		idx[strings.ToLower(r.Hostname)] = r
	}
	return idx
}

// sortedHostnames returns the sorted, lowercased hostnames of the given records.
func sortedHostnames(records []custom_hostnames.CustomHostnameListResponse) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, strings.ToLower(r.Hostname))
	}
	sort.Strings(out)
	return out
}
