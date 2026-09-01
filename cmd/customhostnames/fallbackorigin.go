package customhostnames

import (
	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/custom_hostnames"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	fallbackZone   string
	fallbackDomain string

	setFallbackZone   string
	setFallbackDomain string
	setFallbackOrigin string
	setFallbackDryRun bool
)

// fallbackOriginResult is the serialisable result for --json / --toon output.
type fallbackOriginResult struct {
	Origin string   `json:"origin"           toon:"origin"`
	Status string   `json:"status"           toon:"status"`
	Errors []string `json:"errors,omitempty" toon:"errors,omitempty"`
}

var fallbackOriginCmd = &cobra.Command{
	Use:   "fallback-origin",
	Short: "Show the zone's custom hostname fallback origin",
	Long: `Show the fallback origin that custom hostname traffic is sent to.

Examples:
  cf custom-hostnames fallback-origin --domain example.com
  cf custom-hostnames fallback-origin --zone ZONE_ID --json`,
	RunE: runFallbackOrigin,
}

var setFallbackOriginCmd = &cobra.Command{
	Use:   "set-fallback-origin",
	Short: "Set the zone's custom hostname fallback origin",
	Long: `Set the fallback origin that custom hostname traffic is sent to. The origin
must already exist as an A, AAAA, or CNAME record in the zone.

Examples:
  cf custom-hostnames set-fallback-origin --domain example.com --origin origin.example.com
  cf custom-hostnames set-fallback-origin --domain example.com --origin origin.example.com --dry-run`,
	RunE: runSetFallbackOrigin,
}

func init() {
	fallbackOriginCmd.Flags().StringVar(&fallbackZone, "zone", "", "Zone ID")
	fallbackOriginCmd.Flags().StringVar(&fallbackDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	fallbackOriginCmd.MarkFlagsMutuallyExclusive("zone", "domain")

	setFallbackOriginCmd.Flags().StringVar(&setFallbackZone, "zone", "", "Zone ID")
	setFallbackOriginCmd.Flags().StringVar(&setFallbackDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	setFallbackOriginCmd.Flags().StringVar(&setFallbackOrigin, "origin", "", "Origin hostname to send custom hostname traffic to (required)")
	setFallbackOriginCmd.Flags().BoolVar(&setFallbackDryRun, "dry-run", false, "Show what would change without calling the API")
	setFallbackOriginCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = setFallbackOriginCmd.MarkFlagRequired("origin")
}

func runFallbackOrigin(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, fallbackZone, fallbackDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	p.Info("Fetching fallback origin for zone %s…", ctx.ZoneID)

	resp, err := ctx.Client.CustomHostnames.FallbackOrigin.Get(cmd.Context(), custom_hostnames.FallbackOriginGetParams{
		ZoneID: cf.F(ctx.ZoneID),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := fallbackOriginResult{
		Origin: resp.Origin,
		Status: string(resp.Status),
		Errors: resp.Errors,
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.KV([][2]string{
		{"Origin", dash(result.Origin)},
		{"Status", dash(result.Status)},
	})
	return nil
}

func runSetFallbackOrigin(cmd *cobra.Command, _ []string) error {
	ctx, err := cmdutil.Zone(cmd, setFallbackZone, setFallbackDomain)
	if err != nil {
		return err
	}
	p := ctx.Printer

	if cmdutil.DryRun(p, setFallbackDryRun, "set fallback origin for zone %s to %s", ctx.ZoneID, setFallbackOrigin) {
		return nil
	}

	p.Info("Setting fallback origin for zone %s to %s…", ctx.ZoneID, setFallbackOrigin)

	resp, err := ctx.Client.CustomHostnames.FallbackOrigin.Update(cmd.Context(), custom_hostnames.FallbackOriginUpdateParams{
		ZoneID: cf.F(ctx.ZoneID),
		Origin: cf.F(setFallbackOrigin),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := fallbackOriginResult{
		Origin: resp.Origin,
		Status: string(resp.Status),
		Errors: resp.Errors,
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Fallback origin set")
	p.KV([][2]string{
		{"Origin", dash(result.Origin)},
		{"Status", dash(result.Status)},
	})
	return nil
}
