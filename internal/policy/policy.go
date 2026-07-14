package policy

import (
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/kaushaljayapragash/gatekeeper/internal/config"
	"github.com/kaushaljayapragash/gatekeeper/internal/limiter"
	"github.com/kaushaljayapragash/gatekeeper/internal/limiter/slidingwindow"
	"github.com/kaushaljayapragash/gatekeeper/internal/limiter/tokenbucket"
)

// Check is one scope evaluation: the limiter to call, the key to use, and the cost to deduct.
type Check struct {
	Limiter limiter.Limiter
	Key     string
	Cost    int
}

type compiledScope struct {
	by      string
	limiter limiter.Limiter
	cost    int
}

type compiledPolicy struct {
	path   string
	scopes []compiledScope
}

// Matcher maps an incoming request (path + api key + IP) to the set of Checks that must all pass.
type Matcher struct {
	policies     []compiledPolicy
	defaultAllow bool
}

func New(cfg *config.Config, redisClient *goredis.Client) (*Matcher, error) {
	policies := make([]compiledPolicy, 0, len(cfg.Policies))

	for _, p := range cfg.Policies {
		scopes := make([]compiledScope, 0, len(p.Scopes))
		for _, s := range p.Scopes {
			lim, err := buildLimiter(p.Algorithm, s, redisClient)
			if err != nil {
				return nil, fmt.Errorf("policy %q scope %q: %w", p.Path, s.By, err)
			}
			scopes = append(scopes, compiledScope{
				by:      s.By,
				limiter: lim,
				cost:    s.EffectiveCost(),
			})
		}
		policies = append(policies, compiledPolicy{path: p.Path, scopes: scopes})
	}

	return &Matcher{
		policies:     policies,
		defaultAllow: cfg.DefaultAction == "allow",
	}, nil
}

// Match returns the Checks for the given request. The second return value reports
// whether the request is allowed when no policy matches (the configured default action).
func (m *Matcher) Match(path, apiKey, ip string) ([]Check, bool) {
	for _, p := range m.policies {
		if p.path != path {
			continue
		}
		checks := make([]Check, len(p.scopes))
		for i, s := range p.scopes {
			checks[i] = Check{
				Limiter: s.limiter,
				Key:     buildKey(p.path, s.by, scopeValue(s.by, apiKey, ip)),
				Cost:    s.cost,
			}
		}
		return checks, true
	}
	return nil, m.defaultAllow
}

func scopeValue(by, apiKey, ip string) string {
	if by == "api_key" {
		return apiKey
	}
	return ip
}

// buildKey produces a Redis key that encodes algorithm prefix, path, scope, and identity.
// Example: "tb:/api/fast:api_key:abc123"
func buildKey(path, by, value string) string {
	return path + ":" + by + ":" + value
}

func buildLimiter(algorithm string, s config.Scope, client *goredis.Client) (limiter.Limiter, error) {
	switch algorithm {
	case "token_bucket":
		return tokenbucket.New(client, s.Capacity, s.RefillRate), nil
	case "sliding_window":
		d, err := time.ParseDuration(s.Window)
		if err != nil {
			return nil, fmt.Errorf("invalid window %q: %w", s.Window, err)
		}
		return slidingwindow.New(client, int(d.Seconds()), s.Limit), nil
	default:
		return nil, fmt.Errorf("unknown algorithm %q", algorithm)
	}
}
