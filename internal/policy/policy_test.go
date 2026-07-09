package policy

import (
	"context"
	"testing"

	"github.com/kaushaljayapragash/gatekeeper/internal/config"
	"github.com/kaushaljayapragash/gatekeeper/internal/limiter"
)

// stubLimiter satisfies limiter.Limiter without Redis.
type stubLimiter struct{}

func (s *stubLimiter) Allow(_ context.Context, _ string, _ int) (bool, error) {
	return true, nil
}

// stubMatcher builds a Matcher directly without going through New(), so tests
// don't need a Redis client.
func stubMatcher(policies []compiledPolicy, defaultAllow bool) *Matcher {
	return &Matcher{policies: policies, defaultAllow: defaultAllow}
}

func stub() limiter.Limiter { return &stubLimiter{} }

func TestMatch_knownPath_returnsChecks(t *testing.T) {
	m := stubMatcher([]compiledPolicy{
		{
			path: "/api/fast",
			scopes: []compiledScope{
				{by: "api_key", limiter: stub(), cost: 1},
				{by: "ip", limiter: stub(), cost: 1},
			},
		},
	}, false)

	checks, matched := m.Match("/api/fast", "key-abc", "1.2.3.4")

	if !matched {
		t.Fatal("expected matched=true for known path")
	}
	if len(checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(checks))
	}
}

func TestMatch_unknownPath_defaultDeny(t *testing.T) {
	m := stubMatcher(nil, false)

	checks, allowed := m.Match("/api/unknown", "key-abc", "1.2.3.4")

	if checks != nil {
		t.Fatal("expected nil checks for unknown path")
	}
	if allowed {
		t.Fatal("expected allowed=false with default_action deny")
	}
}

func TestMatch_unknownPath_defaultAllow(t *testing.T) {
	m := stubMatcher(nil, true)

	_, allowed := m.Match("/api/unknown", "key-abc", "1.2.3.4")

	if !allowed {
		t.Fatal("expected allowed=true with default_action allow")
	}
}

func TestMatch_keyIsBuiltFromScopeAndValues(t *testing.T) {
	m := stubMatcher([]compiledPolicy{
		{
			path: "/api/fast",
			scopes: []compiledScope{
				{by: "api_key", limiter: stub(), cost: 1},
				{by: "ip", limiter: stub(), cost: 1},
			},
		},
	}, false)

	checks, _ := m.Match("/api/fast", "key-abc", "1.2.3.4")

	wantKeys := []string{
		"/api/fast:api_key:key-abc",
		"/api/fast:ip:1.2.3.4",
	}
	for i, c := range checks {
		if c.Key != wantKeys[i] {
			t.Errorf("check[%d]: got key %q, want %q", i, c.Key, wantKeys[i])
		}
	}
}

func TestMatch_costDefaultsToOne(t *testing.T) {
	s := config.Scope{Cost: 0}
	if s.EffectiveCost() != 1 {
		t.Fatalf("expected EffectiveCost()=1 when Cost=0, got %d", s.EffectiveCost())
	}
}

func TestMatch_explicitCostPreserved(t *testing.T) {
	m := stubMatcher([]compiledPolicy{
		{
			path: "/api/expensive",
			scopes: []compiledScope{
				{by: "api_key", limiter: stub(), cost: 5},
			},
		},
	}, false)

	checks, _ := m.Match("/api/expensive", "key-abc", "1.2.3.4")

	if checks[0].Cost != 5 {
		t.Fatalf("expected cost=5, got %d", checks[0].Cost)
	}
}

func TestMatch_firstPolicyWins(t *testing.T) {
	m := stubMatcher([]compiledPolicy{
		{path: "/api/fast", scopes: []compiledScope{{by: "api_key", limiter: stub(), cost: 1}}},
		{path: "/api/slow", scopes: []compiledScope{{by: "ip", limiter: stub(), cost: 1}}},
	}, false)

	checks, matched := m.Match("/api/slow", "key-abc", "1.2.3.4")

	if !matched {
		t.Fatal("expected matched=true")
	}
	if checks[0].Key != "/api/slow:ip:1.2.3.4" {
		t.Errorf("unexpected key: %s", checks[0].Key)
	}
}
