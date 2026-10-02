package waf

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	deleteRulesetZone   string
	deleteRulesetDomain string
	deleteRulesetID     string
	deleteRulesetDryRun bool
)

var deleteRulesetCmd = &cobra.Command{
	Use:   "delete-ruleset",
	Short: "Delete an empty zone ruleset",
	Long: `Delete a zone ruleset. Only empty rulesets are deleted; a ruleset that
still has rules is refused, so this cannot drop live rules. Use it to remove
an entrypoint that "cf waf create-rule --phase" created.

Examples:
  cf waf delete-ruleset --domain example.com --ruleset-id RULESET_ID --dry-run`,
	RunE: runDeleteRuleset,
}

func init() {
	deleteRulesetCmd.Flags().StringVar(&deleteRulesetZone, "zone", "", "Zone ID")
	deleteRulesetCmd.Flags().StringVar(&deleteRulesetDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	deleteRulesetCmd.Flags().StringVar(&deleteRulesetID, "ruleset-id", "", "Ruleset ID to delete (required)")
	deleteRulesetCmd.Flags().BoolVar(&deleteRulesetDryRun, "dry-run", false, "Show what would be deleted without calling the API")
	deleteRulesetCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = deleteRulesetCmd.MarkFlagRequired("ruleset-id")
}

func runDeleteRuleset(cmd *cobra.Command, _ []string) error {
	cx, err := cmdutil.Zone(cmd, deleteRulesetZone, deleteRulesetDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	rs, err := cx.Client.Rulesets.Get(cmd.Context(), deleteRulesetID, rulesets.RulesetGetParams{ZoneID: cf.F(cx.ZoneID)})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}
	if n := len(rs.Rules); n > 0 {
		err := fmt.Errorf("ruleset %s (%s) still has %d rule(s); refusing to delete", rs.ID, rs.Phase, n)
		p.Error("%v", err)
		return err
	}
	if cmdutil.DryRun(p, deleteRulesetDryRun, "delete empty %s ruleset %s (%s) in zone %s", rs.Phase, rs.ID, rs.Name, cx.ZoneID) {
		return nil
	}
	if err := cx.Client.Rulesets.Delete(cmd.Context(), deleteRulesetID, rulesets.RulesetDeleteParams{ZoneID: cf.F(cx.ZoneID)}); err != nil {
		p.Error("API error: %v", err)
		return err
	}
	p.Success("Deleted empty %s ruleset %s", rs.Phase, rs.ID)
	return nil
}
