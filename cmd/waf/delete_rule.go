package waf

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
)

var (
	deleteRuleZone      string
	deleteRuleDomain    string
	deleteRuleRulesetID string
	deleteRuleRuleID    string
	deleteRuleDryRun    bool
)

// deleteRuleResult is the serialisable result of a rule deletion.
type deleteRuleResult struct {
	RulesetID      string `json:"ruleset_id"      toon:"ruleset_id"`
	RulesetName    string `json:"ruleset_name"    toon:"ruleset_name"`
	RulesetVersion string `json:"ruleset_version" toon:"ruleset_version"`
	DeletedRuleID  string `json:"deleted_rule_id" toon:"deleted_rule_id"`
	RemainingRules int    `json:"remaining_rules" toon:"remaining_rules"`
}

var deleteRuleCmd = &cobra.Command{
	Use:   "delete-rule",
	Short: "Delete a rule from a ruleset",
	Long: `Delete a single rule from a ruleset by its rule ID.

This uses the per-rule endpoint, so nothing else in the ruleset is rewritten and
a concurrent edit cannot be clobbered.

Examples:
  cf waf delete-rule --domain example.com --ruleset-id RULESET_ID --rule-id RULE_ID
  cf waf delete-rule --zone ZONE_ID --ruleset-id RULESET_ID --rule-id RULE_ID --dry-run
  cf waf delete-rule --zone ZONE_ID --ruleset-id RULESET_ID --rule-id RULE_ID --json`,
	RunE: runDeleteRule,
}

func init() {
	deleteRuleCmd.Flags().StringVar(&deleteRuleZone, "zone", "", "Zone ID")
	deleteRuleCmd.Flags().StringVar(&deleteRuleDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	deleteRuleCmd.Flags().StringVar(&deleteRuleRulesetID, "ruleset-id", "", "Ruleset ID containing the rule (required)")
	deleteRuleCmd.Flags().StringVar(&deleteRuleRuleID, "rule-id", "", "Rule ID to delete (required)")
	deleteRuleCmd.Flags().BoolVar(&deleteRuleDryRun, "dry-run", false, "Show what would be deleted without calling the API")

	deleteRuleCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = deleteRuleCmd.MarkFlagRequired("ruleset-id")
	_ = deleteRuleCmd.MarkFlagRequired("rule-id")
}

func runDeleteRule(cmd *cobra.Command, _ []string) error {
	cx, err := cmdutil.Zone(cmd, deleteRuleZone, deleteRuleDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	if cmdutil.DryRun(p, deleteRuleDryRun,
		"delete rule %s from ruleset %s in zone %s",
		deleteRuleRuleID, deleteRuleRulesetID, cx.ZoneID) {
		return nil
	}

	p.Info("Deleting rule %s from ruleset %s…", deleteRuleRuleID, deleteRuleRulesetID)

	res, err := cx.Client.Rulesets.Rules.Delete(cmd.Context(), deleteRuleRulesetID, deleteRuleRuleID,
		rulesets.RuleDeleteParams{ZoneID: cf.F(cx.ZoneID)})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := deleteRuleResult{
		RulesetID:      res.ID,
		RulesetName:    res.Name,
		RulesetVersion: res.Version,
		DeletedRuleID:  deleteRuleRuleID,
		RemainingRules: len(res.Rules),
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Rule deleted")
	p.KV([][2]string{
		{"Ruleset", result.RulesetName},
		{"Ruleset ID", result.RulesetID},
		{"Ruleset version", result.RulesetVersion},
		{"Deleted rule", result.DeletedRuleID},
		{"Remaining rules", fmt.Sprintf("%d", result.RemainingRules)},
	})
	return nil
}
