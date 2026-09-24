package waf

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/ruleinfo"
)

var (
	updateRuleZone        string
	updateRuleDomain      string
	updateRuleRulesetID   string
	updateRuleRuleID      string
	updateRuleExpression  string
	updateRuleAction      string
	updateRuleDescription string
	updateRuleEnabled     bool
	updateRuleDryRun      bool
)

// readOnlyRuleFields are returned when reading a rule but are not part of the
// writable rule definition.
var readOnlyRuleFields = []string{"id", "version", "last_updated", "categories"}

// updateRuleResult is the serialisable result of a rule update.
type updateRuleResult struct {
	RulesetID      string     `json:"ruleset_id"      toon:"ruleset_id"`
	RulesetName    string     `json:"ruleset_name"    toon:"ruleset_name"`
	RulesetVersion string     `json:"ruleset_version" toon:"ruleset_version"`
	Previous       ruleResult `json:"previous"        toon:"previous"`
	Rule           ruleResult `json:"rule"            toon:"rule"`
}

var updateRuleCmd = &cobra.Command{
	Use:   "update-rule",
	Short: "Change an existing rule in place",
	Long: `Change the expression, action, description or enabled state of an existing
rule, keeping its ID and position.

The Cloudflare endpoint replaces the whole rule definition, so the current rule
is read first and only the flags you pass are changed. Everything else,
including action parameters and logging settings, is sent back as it was.

Actions: block, challenge, js_challenge, managed_challenge, log, skip

Examples:
  cf waf update-rule --domain example.com --ruleset-id RULESET_ID --rule-id RULE_ID \
    --expression 'http.host eq "app.example.com" and ip.src.country eq "RU"'
  cf waf update-rule --zone ZONE_ID --ruleset-id RULESET_ID --rule-id RULE_ID --enabled=false
  cf waf update-rule --zone ZONE_ID --ruleset-id RULESET_ID --rule-id RULE_ID \
    --action managed_challenge --dry-run`,
	RunE: runUpdateRule,
}

func init() {
	updateRuleCmd.Flags().StringVar(&updateRuleZone, "zone", "", "Zone ID")
	updateRuleCmd.Flags().StringVar(&updateRuleDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	updateRuleCmd.Flags().StringVar(&updateRuleRulesetID, "ruleset-id", "", "Ruleset ID containing the rule (required)")
	updateRuleCmd.Flags().StringVar(&updateRuleRuleID, "rule-id", "", "Rule ID to update (required)")
	updateRuleCmd.Flags().StringVar(&updateRuleExpression, "expression", "", "New match expression")
	updateRuleCmd.Flags().StringVar(&updateRuleAction, "action", "", "New action: "+actionList())
	updateRuleCmd.Flags().StringVar(&updateRuleDescription, "description", "", "New description")
	updateRuleCmd.Flags().BoolVar(&updateRuleEnabled, "enabled", true, "Whether the rule is active")
	updateRuleCmd.Flags().BoolVar(&updateRuleDryRun, "dry-run", false, "Show the change without calling the API")

	updateRuleCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	updateRuleCmd.MarkFlagsOneRequired("expression", "action", "description", "enabled")
	_ = updateRuleCmd.MarkFlagRequired("ruleset-id")
	_ = updateRuleCmd.MarkFlagRequired("rule-id")
}

func runUpdateRule(cmd *cobra.Command, _ []string) error {
	flags := cmd.Flags()

	// Validate the action before resolving the zone so a typo costs no API call.
	var action string
	if flags.Changed("action") {
		a, ok := validActions[strings.ToLower(updateRuleAction)]
		if !ok {
			p, _ := cmdutil.Setup(cmd)
			err := fmt.Errorf("invalid --action %q; valid values: %s", updateRuleAction, actionList())
			p.Error("%v", err)
			return err
		}
		action = string(a)
	}

	cx, err := cmdutil.Zone(cmd, updateRuleZone, updateRuleDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	p.Info("Fetching rule %s from ruleset %s…", updateRuleRuleID, updateRuleRulesetID)

	current, err := cx.Client.Rulesets.Get(cmd.Context(), updateRuleRulesetID, rulesets.RulesetGetParams{
		ZoneID: cf.F(cx.ZoneID),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	var body map[string]interface{}
	var previous ruleResult
	for i, r := range current.Rules {
		if r.ID != updateRuleRuleID {
			continue
		}
		if err := json.Unmarshal([]byte(r.JSON.RawJSON()), &body); err != nil {
			p.Error("could not decode rule %s: %v", r.ID, err)
			return err
		}
		previous = ruleResult{
			Index:       i + 1,
			ID:          r.ID,
			Description: r.Description,
			Action:      string(r.Action),
			Enabled:     r.Enabled,
			Expression:  r.Expression,
			Categories:  ruleinfo.Categories(r.Categories),
			Version:     r.Version,
			LastUpdated: r.LastUpdated.String(),
		}
		break
	}
	if body == nil {
		err := fmt.Errorf("rule %s not found in ruleset %s", updateRuleRuleID, updateRuleRulesetID)
		p.Error("%v", err)
		return err
	}

	for _, f := range readOnlyRuleFields {
		delete(body, f)
	}
	var changes []string
	if flags.Changed("expression") {
		body["expression"] = updateRuleExpression
		changes = append(changes, fmt.Sprintf("expression: %s → %s", previous.Expression, updateRuleExpression))
	}
	if flags.Changed("action") {
		body["action"] = action
		changes = append(changes, fmt.Sprintf("action: %s → %s", previous.Action, action))
	}
	if flags.Changed("description") {
		body["description"] = updateRuleDescription
		changes = append(changes, fmt.Sprintf("description: %q → %q", previous.Description, updateRuleDescription))
	}
	if flags.Changed("enabled") {
		body["enabled"] = updateRuleEnabled
		changes = append(changes, fmt.Sprintf("enabled: %v → %v", previous.Enabled, updateRuleEnabled))
	}

	if cmdutil.DryRun(p, updateRuleDryRun,
		"update rule %s (position %d) in ruleset %s in zone %s:\n  %s",
		updateRuleRuleID, previous.Index, updateRuleRulesetID, cx.ZoneID, strings.Join(changes, "\n  ")) {
		return nil
	}

	payload, err := json.Marshal(body)
	if err != nil {
		p.Error("could not encode rule: %v", err)
		return err
	}

	p.Info("Updating rule %s…", updateRuleRuleID)

	var env rulesets.RuleEditResponseEnvelope
	path := fmt.Sprintf("zones/%s/rulesets/%s/rules/%s", cx.ZoneID, updateRuleRulesetID, updateRuleRuleID)
	if err := cx.Client.Patch(cmd.Context(), path, json.RawMessage(payload), &env); err != nil {
		p.Error("API error: %v", err)
		return err
	}
	res := env.Result

	result := updateRuleResult{
		RulesetID:      res.ID,
		RulesetName:    res.Name,
		RulesetVersion: res.Version,
		Previous:       previous,
	}
	for i, r := range res.Rules {
		if r.ID == updateRuleRuleID {
			result.Rule = ruleResult{
				Index:       i + 1,
				ID:          r.ID,
				Description: r.Description,
				Action:      string(r.Action),
				Enabled:     r.Enabled,
				Expression:  r.Expression,
				Categories:  ruleinfo.Categories(r.Categories),
				Version:     r.Version,
				LastUpdated: r.LastUpdated.String(),
			}
			break
		}
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	p.Success("Rule updated")
	p.KV([][2]string{
		{"Ruleset", fmt.Sprintf("%s (%s)", result.RulesetName, result.RulesetID)},
		{"Ruleset version", result.RulesetVersion},
		{"Rule ID", result.Rule.ID},
		{"Position", fmt.Sprintf("%d of %d", result.Rule.Index, len(res.Rules))},
		{"Rule version", fmt.Sprintf("%s → %s", previous.Version, result.Rule.Version)},
		{"Description", result.Rule.Description},
		{"Action", result.Rule.Action},
		{"Enabled", fmt.Sprintf("%v", result.Rule.Enabled)},
		{"Expression", result.Rule.Expression},
	})
	return nil
}
