package waf

import (
	"testing"

	"github.com/cloudflare/cloudflare-go/v6/rulesets"
)

func TestRedirectIsValidAndNeedsParams(t *testing.T) {
	a, ok := validActions["redirect"]
	if !ok || a != rulesets.RuleNewParamsBodyActionRedirect {
		t.Fatalf("redirect not mapped to the SDK redirect action: %v %v", a, ok)
	}
	if !requiresActionParams(a) {
		t.Error("redirect must require --action-parameters")
	}
	if !requiresActionParams(rulesets.RuleNewParamsBodyActionSetCacheSettings) {
		t.Error("set_cache_settings must require --action-parameters")
	}
	if requiresActionParams(rulesets.RuleNewParamsBodyActionBlock) {
		t.Error("block must not require --action-parameters")
	}
}
