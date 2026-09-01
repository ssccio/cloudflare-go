package waf

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/ruleinfo"
)

var (
	createRuleZone        string
	createRuleDomain      string
	createRuleRulesetID   string
	createRuleExpression  string
	createRuleAction      string
	createRuleDescription string
	createRuleEnabled     bool
	createRuleBefore      string
	createRuleDryRun      bool
)

// validActions maps the accepted --action values to the SDK enum. Only the
// actions that make sense for a hand-written zone WAF rule are offered.
var validActions = map[string]rulesets.RuleNewParamsBodyAction{
	"block":             rulesets.RuleNewParamsBodyActionBlock,
	"challenge":         rulesets.RuleNewParamsBodyActionChallenge,
	"js_challenge":      rulesets.RuleNewParamsBodyActionJSChallenge,
	"managed_challenge": rulesets.RuleNewParamsBodyActionManagedChallenge,
	"log":               rulesets.RuleNewParamsBodyActionLog,
	"skip":              rulesets.RuleNewParamsBodyActionSkip,
}

// createRuleResult is the serialisable result of a rule creation.
type createRuleResult struct {
	RulesetID      string     `json:"ruleset_id"        toon:"ruleset_id"`
	RulesetName    string     `json:"ruleset_name"      toon:"ruleset_name"`
	RulesetVersion string     `json:"ruleset_version"   toon:"ruleset_version"`
	Rule           ruleResult `json:"rule"              toon:"rule"`
}

var createRuleCmd = &cobra.Command{
	Use:   "create-rule",
	Short: "Add a rule to a ruleset",
	Long: `Add a single rule to an existing ruleset.

This uses the per-rule endpoint, so only the new rule is sent. It does not read
and rewrite the whole ruleset, which would race with concurrent edits and can
silently drop rules.

Actions: block, challenge, js_challenge, managed_challenge, log, skip

Use --before with an existing rule ID to insert ahead of that rule, which is how
a skip rule is placed in front of the execute rules it needs to pre-empt.

Examples:
  cf waf create-rule --domain example.com --ruleset-id RULESET_ID \
    --expression 'ip.src eq 203.0.113.5' --action block --description 'Block scraper'
  cf waf create-rule --zone ZONE_ID --ruleset-id RULESET_ID \
    --expression 'ip.src in {203.0.113.0/24}' --action skip \
    --description 'Allowlist office' --before EXISTING_RULE_ID
  cf waf create-rule --zone ZONE_ID --ruleset-id RULESET_ID \
    --expression 'http.host eq "api.example.com"' --action log \
    --description 'Observe API' --dry-run`,
	RunE: runCreateRule,
}

func init() {
	createRuleCmd.Flags().StringVar(&createRuleZone, "zone", "", "Zone ID")
	createRuleCmd.Flags().StringVar(&createRuleDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	createRuleCmd.Flags().StringVar(&createRuleRulesetID, "ruleset-id", "", "Ruleset ID to add the rule to (required)")
	createRuleCmd.Flags().StringVar(&createRuleExpression, "expression", "", "Match expression in Cloudflare filter syntax (required)")
	createRuleCmd.Flags().StringVar(&createRuleAction, "action", "", "Action: "+actionList()+" (required)")
	createRuleCmd.Flags().StringVar(&createRuleDescription, "description", "", "Human-readable rule description (required)")
	createRuleCmd.Flags().BoolVar(&createRuleEnabled, "enabled", true, "Whether the rule is active")
	createRuleCmd.Flags().StringVar(&createRuleBefore, "before", "", "Insert ahead of this existing rule ID")
	createRuleCmd.Flags().BoolVar(&createRuleDryRun, "dry-run", false, "Show what would be created without calling the API")

	createRuleCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = createRuleCmd.MarkFlagRequired("ruleset-id")
	_ = createRuleCmd.MarkFlagRequired("expression")
	_ = createRuleCmd.MarkFlagRequired("action")
	_ = createRuleCmd.MarkFlagRequired("description")
}

func runCreateRule(cmd *cobra.Command, _ []string) error {
	// Validate the action before resolving the zone so a typo costs no API call.
	action, ok := validActions[strings.ToLower(createRuleAction)]
	if !ok {
		p, _ := cmdutil.Setup(cmd)
		err := fmt.Errorf("invalid --action %q; valid values: %s", createRuleAction, actionList())
		p.Error("%v", err)
		return err
	}

	cx, err := cmdutil.Zone(cmd, createRuleZone, createRuleDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	position := ""
	if createRuleBefore != "" {
		position = fmt.Sprintf(" before rule %s", createRuleBefore)
	}
	if cmdutil.DryRun(p, createRuleDryRun,
		"add %s rule %q to ruleset %s in zone %s%s with expression: %s",
		action, createRuleDescription, createRuleRulesetID, cx.ZoneID, position, createRuleExpression) {
		return nil
	}

	body := rulesets.RuleNewParamsBody{
		Action:      cf.F(action),
		Description: cf.F(createRuleDescription),
		Enabled:     cf.F(createRuleEnabled),
		Expression:  cf.F(createRuleExpression),
	}
	if createRuleBefore != "" {
		body.Position = cf.F[interface{}](map[string]interface{}{"before": createRuleBefore})
	}

	p.Info("Adding rule to ruleset %s in zone %s…", createRuleRulesetID, cx.ZoneID)

	res, err := cx.Client.Rulesets.Rules.New(cmd.Context(), createRuleRulesetID, rulesets.RuleNewParams{
		ZoneID: cf.F(cx.ZoneID),
		Body:   body,
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := createRuleResult{
		RulesetID:      res.ID,
		RulesetName:    res.Name,
		RulesetVersion: res.Version,
	}

	// The endpoint returns the whole updated ruleset, not just the new rule, so
	// locate the rule we asked for by its description and expression.
	for i, r := range res.Rules {
		if r.Description == createRuleDescription && r.Expression == createRuleExpression {
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

	p.Success("Rule created")
	p.KV([][2]string{
		{"Ruleset", fmt.Sprintf("%s (%s)", result.RulesetName, result.RulesetID)},
		{"Ruleset version", result.RulesetVersion},
		{"Rule ID", result.Rule.ID},
		{"Position", fmt.Sprintf("%d of %d", result.Rule.Index, len(res.Rules))},
		{"Description", result.Rule.Description},
		{"Action", result.Rule.Action},
		{"Enabled", fmt.Sprintf("%v", result.Rule.Enabled)},
		{"Expression", result.Rule.Expression},
	})
	return nil
}

// actionList renders the accepted --action values in a stable order.
func actionList() string {
	names := make([]string, 0, len(validActions))
	for name := range validActions {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
