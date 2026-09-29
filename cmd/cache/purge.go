package cache

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	cfcache "github.com/cloudflare/cloudflare-go/v6/cache"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	purgeZone   string
	purgeDomain string
	purgeURLs   []string
	purgeDryRun bool
)

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Purge specific URLs from a zone's cache",
	Long: `Purge one or more URLs from a zone's cache (purge by single file).

For a Cloudflare for SaaS custom hostname, purge from the SaaS zone the
hostname lives in, using the custom hostname's URL.

Examples:
  cf cache purge --domain example.com --url https://www.example.com/file.pdf
  cf cache purge --domain ethosce.cf.gocadmium.com --url https://lms.client.com/a.pdf --url https://lms.client.com/b.pdf
  cf cache purge --domain example.com --url https://www.example.com/file.pdf --dry-run`,
	RunE: runPurge,
}

func init() {
	purgeCmd.Flags().StringVar(&purgeZone, "zone", "", "Zone ID")
	purgeCmd.Flags().StringVar(&purgeDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	purgeCmd.Flags().StringArrayVar(&purgeURLs, "url", nil, "Full URL to purge, including scheme (repeatable, required)")
	purgeCmd.Flags().BoolVar(&purgeDryRun, "dry-run", false, "Show what would be purged without making the API call")

	purgeCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = purgeCmd.MarkFlagRequired("url")
}

func runPurge(cmd *cobra.Command, _ []string) error {
	// The API accepts a scheme-less URL but matches it against the cache key,
	// so a missing scheme quietly purges nothing useful.
	for _, u := range purgeURLs {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return fmt.Errorf("--url %q must include http:// or https://", u)
		}
	}

	c, err := cmdutil.Zone(cmd, purgeZone, purgeDomain)
	if err != nil {
		return err
	}
	p := c.Printer

	if cmdutil.DryRun(p, purgeDryRun, "purge %d URL(s) from zone %s: %s", len(purgeURLs), c.ZoneID, strings.Join(purgeURLs, ", ")) {
		return nil
	}

	res, err := c.Client.Cache.Purge(cmd.Context(), cfcache.CachePurgeParams{
		ZoneID: cf.F(c.ZoneID),
		Body:   cfcache.CachePurgeParamsBodyCachePurgeSingleFile{Files: cf.F(purgeURLs)},
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := map[string]any{"id": res.ID, "zone_id": c.ZoneID, "purged": purgeURLs}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Purge accepted (id %s) for %d URL(s)", res.ID, len(purgeURLs))
	return nil
}
