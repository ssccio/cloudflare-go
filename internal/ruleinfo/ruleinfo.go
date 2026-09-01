// Package ruleinfo resolves Cloudflare rule IDs to human-readable rule
// descriptions. Firewall events carry only a rulesetId/ruleId pair, which is
// useless to an operator on its own; this turns it into the rule's name.
package ruleinfo

import (
	"context"
	"fmt"
	"sync"

	cf "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/rulesets"
)

// Info describes a single rule within a ruleset.
type Info struct {
	RuleDescription string   `json:"rule_description" toon:"rule_description"`
	RuleCategories  []string `json:"rule_categories"  toon:"rule_categories"`
	RulesetName     string   `json:"ruleset_name"     toon:"ruleset_name"`
	RulesetPhase    string   `json:"ruleset_phase"    toon:"ruleset_phase"`
}

// Resolver looks up rule metadata, caching each ruleset fetch for the process
// lifetime so a page of events costs one API call per distinct ruleset.
type Resolver struct {
	client *cf.Client
	zoneID string

	mu    sync.Mutex
	cache map[string]map[string]Info
}

// NewResolver builds a Resolver scoped to one zone.
func NewResolver(c *cf.Client, zoneID string) *Resolver {
	return &Resolver{client: c, zoneID: zoneID, cache: map[string]map[string]Info{}}
}

// Lookup returns the Info for a rule. The bool reports whether the rule was
// found; a miss is not an error, since some event sources (l7ddos, ratelimit,
// botFight) carry rule IDs that belong to no retrievable ruleset.
func (r *Resolver) Lookup(ctx context.Context, rulesetID, ruleID string) (Info, bool, error) {
	if rulesetID == "" || ruleID == "" {
		return Info{}, false, nil
	}

	r.mu.Lock()
	rules, cached := r.cache[rulesetID]
	r.mu.Unlock()

	if !cached {
		var err error
		rules, err = r.fetch(ctx, rulesetID)
		if err != nil {
			return Info{}, false, err
		}
		r.mu.Lock()
		r.cache[rulesetID] = rules
		r.mu.Unlock()
	}

	info, ok := rules[ruleID]
	return info, ok, nil
}

// fetch retrieves one ruleset and indexes its rules by ID.
func (r *Resolver) fetch(ctx context.Context, rulesetID string) (map[string]Info, error) {
	res, err := r.client.Rulesets.Get(ctx, rulesetID, rulesets.RulesetGetParams{
		ZoneID: cf.F(r.zoneID),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching ruleset %s: %w", rulesetID, err)
	}

	out := make(map[string]Info, len(res.Rules))
	for _, rule := range res.Rules {
		out[rule.ID] = Info{
			RuleDescription: rule.Description,
			RuleCategories:  Categories(rule.Categories),
			RulesetName:     res.Name,
			RulesetPhase:    string(res.Phase),
		}
	}
	return out, nil
}

// Categories converts the SDK's interface{} categories field to a string slice.
// The value arrives as []string when the rule matched a typed union member and
// as []interface{} when it fell through to a generic decode, so handle both.
func Categories(v interface{}) []string {
	switch raw := v.(type) {
	case []string:
		return raw
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
