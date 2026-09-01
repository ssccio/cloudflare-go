package waf

import (
	"fmt"

	"github.com/spf13/cobra"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"

	"github.com/ssccio/cloudflare-go/internal/cmdutil"
	"github.com/ssccio/cloudflare-go/internal/ruleinfo"
)

var (
	listRulesZone      string
	listRulesDomain    string
	listRulesRulesetID string
)

// ruleResult is the serialisable form of a single rule. Expression is carried
// verbatim; only the human table abbreviates it.
type ruleResult struct {
	Index       int      `json:"index"                  toon:"index"`
	ID          string   `json:"id"                     toon:"id"`
	Description string   `json:"description"            toon:"description"`
	Action      string   `json:"action"                 toon:"action"`
	Enabled     bool     `json:"enabled"                toon:"enabled"`
	Expression  string   `json:"expression"             toon:"expression"`
	Categories  []string `json:"categories,omitempty"   toon:"categories,omitempty"`
	Version     string   `json:"version,omitempty"      toon:"version,omitempty"`
	LastUpdated string   `json:"last_updated,omitempty" toon:"last_updated,omitempty"`
}

// rulesetRulesResult wraps the ruleset metadata with its rules.
type rulesetRulesResult struct {
	RulesetID string       `json:"ruleset_id" toon:"ruleset_id"`
	Name      string       `json:"name"       toon:"name"`
	Phase     string       `json:"phase"      toon:"phase"`
	Kind      string       `json:"kind"       toon:"kind"`
	Version   string       `json:"version"    toon:"version"`
	Rules     []ruleResult `json:"rules"      toon:"rules"`
}

var listRulesCmd = &cobra.Command{
	Use:   "list-rules",
	Short: "List the rules inside a ruleset",
	Long: `List every rule in a ruleset, in evaluation order, with its ID, action,
enabled state and full match expression.

Get the ruleset ID from ` + "`cf waf list-rulesets`" + `.

Examples:
  cf waf list-rules --domain example.com --ruleset-id RULESET_ID
  cf waf list-rules --zone ZONE_ID --ruleset-id RULESET_ID --json
  cf waf list-rules --zone ZONE_ID --ruleset-id RULESET_ID --json --query 'rules[?enabled].{id: id, expr: expression}'`,
	RunE: runListRules,
}

func init() {
	listRulesCmd.Flags().StringVar(&listRulesZone, "zone", "", "Zone ID")
	listRulesCmd.Flags().StringVar(&listRulesDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	listRulesCmd.Flags().StringVar(&listRulesRulesetID, "ruleset-id", "", "Ruleset ID (required)")
	listRulesCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	_ = listRulesCmd.MarkFlagRequired("ruleset-id")
}

func runListRules(cmd *cobra.Command, _ []string) error {
	cx, err := cmdutil.Zone(cmd, listRulesZone, listRulesDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	p.Info("Fetching ruleset %s in zone %s…", listRulesRulesetID, cx.ZoneID)

	res, err := cx.Client.Rulesets.Get(cmd.Context(), listRulesRulesetID, rulesets.RulesetGetParams{
		ZoneID: cf.F(cx.ZoneID),
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := rulesetRulesResult{
		RulesetID: res.ID,
		Name:      res.Name,
		Phase:     string(res.Phase),
		Kind:      string(res.Kind),
		Version:   res.Version,
		Rules:     make([]ruleResult, 0, len(res.Rules)),
	}
	for i, r := range res.Rules {
		result.Rules = append(result.Rules, ruleResult{
			Index:       i + 1,
			ID:          r.ID,
			Description: r.Description,
			Action:      string(r.Action),
			Enabled:     r.Enabled,
			Expression:  r.Expression,
			Categories:  ruleinfo.Categories(r.Categories),
			Version:     r.Version,
			LastUpdated: r.LastUpdated.String(),
		})
	}

	if p.JSON || p.TOON {
		p.PrintResult(result)
		return nil
	}

	if len(result.Rules) == 0 {
		p.Info("Ruleset %s (%s) contains no rules.", res.ID, res.Name)
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s  (%s / %s)\n", result.Name, result.Phase, result.Kind)
	for _, r := range result.Rules {
		fmt.Fprintf(out, "\n%d.\n", r.Index)
		p.KV([][2]string{
			{"Description", r.Description},
			{"ID", r.ID},
			{"Action", r.Action},
			{"Enabled", fmt.Sprintf("%v", r.Enabled)},
			{"Expression", r.Expression},
		})
	}
	fmt.Fprintf(out, "\n  %d rule(s)\n", len(result.Rules))
	return nil
}
