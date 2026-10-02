package waf

import (
	"errors"
	"fmt"
	"net/http"
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
	createRulePhase       string
	createRuleExpression  string
	createRuleAction      string
	createRuleDescription string
	createRuleEnabled     bool
	createRuleBefore      string
	createRuleDryRun      bool

	createRuleRLCharacteristics []string
	createRuleRLPeriod          int
	createRuleRLRequests        int
	createRuleRLTimeout         int
	createRuleRLCounting        string
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
	RulesetCreated bool       `json:"ruleset_created"   toon:"ruleset_created"`
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

Rate-limit rules (http_ratelimit phase) take --rl-period and --rl-requests;
--rl-characteristics defaults to ip.src and always includes cf.colo.id, which
Cloudflare requires. --rl-counting-expression counts a different set of requests
than the ones the rule acts on.

Instead of --ruleset-id, --phase names the zone entrypoint ruleset for that
phase (e.g. http_ratelimit, http_request_firewall_custom). If the zone has no
entrypoint for the phase yet, an empty one is created first and the rule is
added to it. "cf waf delete-ruleset" removes it again once it is empty.

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
    --description 'Observe API' --dry-run
  cf waf create-rule --zone ZONE_ID --ruleset-id RATELIMIT_RULESET_ID \
    --expression 'not starts_with(http.request.uri.path, "/api/")' --action managed_challenge \
    --description 'Per-ASN rate limit' --rl-characteristics ip.geoip.asnum \
    --rl-period 60 --rl-requests 600 --rl-mitigation-timeout 600 --dry-run`,
	RunE: runCreateRule,
}

func init() {
	createRuleCmd.Flags().StringVar(&createRuleZone, "zone", "", "Zone ID")
	createRuleCmd.Flags().StringVar(&createRuleDomain, "domain", "", "Domain name (resolved to zone ID automatically)")
	createRuleCmd.Flags().StringVar(&createRuleRulesetID, "ruleset-id", "", "Ruleset ID to add the rule to (this or --phase is required)")
	createRuleCmd.Flags().StringVar(&createRulePhase, "phase", "", "Zone entrypoint phase to add the rule to; created if missing")
	createRuleCmd.Flags().StringVar(&createRuleExpression, "expression", "", "Match expression in Cloudflare filter syntax (required)")
	createRuleCmd.Flags().StringVar(&createRuleAction, "action", "", "Action: "+actionList()+" (required)")
	createRuleCmd.Flags().StringVar(&createRuleDescription, "description", "", "Human-readable rule description (required)")
	createRuleCmd.Flags().BoolVar(&createRuleEnabled, "enabled", true, "Whether the rule is active")
	createRuleCmd.Flags().StringVar(&createRuleBefore, "before", "", "Insert ahead of this existing rule ID")
	createRuleCmd.Flags().BoolVar(&createRuleDryRun, "dry-run", false, "Show what would be created without calling the API")

	createRuleCmd.Flags().StringSliceVar(&createRuleRLCharacteristics, "rl-characteristics", nil, "Rate limit: fields to count by (default ip.src; cf.colo.id is always added)")
	createRuleCmd.Flags().IntVar(&createRuleRLPeriod, "rl-period", 0, "Rate limit: counting period in seconds (10, 60, 120, 300, 600, 3600)")
	createRuleCmd.Flags().IntVar(&createRuleRLRequests, "rl-requests", 0, "Rate limit: requests allowed per period")
	createRuleCmd.Flags().IntVar(&createRuleRLTimeout, "rl-mitigation-timeout", 0, "Rate limit: seconds the action stays applied once triggered (0 = only while over the limit)")
	createRuleCmd.Flags().StringVar(&createRuleRLCounting, "rl-counting-expression", "", "Rate limit: expression selecting which requests count (default: the rule expression)")

	createRuleCmd.MarkFlagsMutuallyExclusive("zone", "domain")
	createRuleCmd.MarkFlagsRequiredTogether("rl-period", "rl-requests")
	createRuleCmd.MarkFlagsMutuallyExclusive("ruleset-id", "phase")
	createRuleCmd.MarkFlagsOneRequired("ruleset-id", "phase")
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

	ratelimit := buildRatelimit()

	cx, err := cmdutil.Zone(cmd, createRuleZone, createRuleDomain)
	if err != nil {
		return err
	}
	p := cx.Printer

	rlDesc := ""
	if ratelimit != nil {
		rlDesc = fmt.Sprintf(" and rate limit %d requests/%ds by %s (mitigation %ds)",
			createRuleRLRequests, createRuleRLPeriod,
			strings.Join(ratelimit["characteristics"].([]string), "+"), createRuleRLTimeout)
		if createRuleRLCounting != "" {
			rlDesc += fmt.Sprintf(", counting: %s", createRuleRLCounting)
		}
	}

	rulesetID, created := createRuleRulesetID, false
	if createRulePhase != "" {
		ep, err := cx.Client.Rulesets.Phases.Get(cmd.Context(), rulesets.Phase(createRulePhase), rulesets.PhaseGetParams{ZoneID: cf.F(cx.ZoneID)})
		var apiErr *cf.Error
		switch {
		case err == nil:
			rulesetID = ep.ID
		case errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound:
			created = true
		default:
			p.Error("API error: %v", err)
			return err
		}
	}
	target := "ruleset " + rulesetID
	if created {
		target = "a new empty " + createRulePhase + " entrypoint ruleset"
	}

	position := ""
	if createRuleBefore != "" {
		position = fmt.Sprintf(" before rule %s", createRuleBefore)
	}
	if cmdutil.DryRun(p, createRuleDryRun,
		"add %s rule %q to %s in zone %s%s with expression: %s%s",
		action, createRuleDescription, target, cx.ZoneID, position, createRuleExpression, rlDesc) {
		return nil
	}

	body := rulesets.RuleNewParamsBody{
		Action:      cf.F(action),
		Description: cf.F(createRuleDescription),
		Enabled:     cf.F(createRuleEnabled),
		Expression:  cf.F(createRuleExpression),
	}
	if ratelimit != nil {
		body.Ratelimit = cf.F[interface{}](ratelimit)
	}
	if createRuleBefore != "" {
		body.Position = cf.F[interface{}](map[string]interface{}{"before": createRuleBefore})
	}

	if created {
		rs, err := cx.Client.Rulesets.New(cmd.Context(), rulesets.RulesetNewParams{
			ZoneID: cf.F(cx.ZoneID),
			Kind:   cf.F(rulesets.KindZone),
			Name:   cf.F("default"),
			Phase:  cf.F(rulesets.Phase(createRulePhase)),
			Rules:  cf.F([]rulesets.RulesetNewParamsRuleUnion{}),
		})
		if err != nil {
			p.Error("API error creating %s entrypoint: %v", createRulePhase, err)
			return err
		}
		rulesetID = rs.ID
		p.Info("Created empty %s entrypoint ruleset %s.", createRulePhase, rulesetID)
	}

	p.Info("Adding rule to ruleset %s in zone %s…", rulesetID, cx.ZoneID)

	res, err := cx.Client.Rulesets.Rules.New(cmd.Context(), rulesetID, rulesets.RuleNewParams{
		ZoneID: cf.F(cx.ZoneID),
		Body:   body,
	})
	if err != nil {
		p.Error("API error: %v", err)
		return err
	}

	result := createRuleResult{
		RulesetCreated: created,
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

// buildRatelimit returns the ratelimit object for the request body, or nil when
// no rate-limit flags were given.
func buildRatelimit() map[string]interface{} {
	if createRuleRLPeriod == 0 {
		return nil
	}
	chars := []string{}
	for _, c := range createRuleRLCharacteristics {
		if c != "cf.colo.id" {
			chars = append(chars, c)
		}
	}
	if len(chars) == 0 {
		chars = append(chars, "ip.src")
	}
	chars = append(chars, "cf.colo.id")

	rl := map[string]interface{}{
		"characteristics":     chars,
		"period":              createRuleRLPeriod,
		"requests_per_period": createRuleRLRequests,
		"mitigation_timeout":  createRuleRLTimeout,
	}
	if createRuleRLCounting != "" {
		rl["counting_expression"] = createRuleRLCounting
	}
	return rl
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
