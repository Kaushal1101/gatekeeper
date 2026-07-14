package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DefaultAction  string   `yaml:"default_action"`
	OnLimiterError string   `yaml:"on_limiter_error"`
	Policies       []Policy `yaml:"policies"`
}

type Policy struct {
	Path      string  `yaml:"path"`
	Algorithm string  `yaml:"algorithm"`
	Scopes    []Scope `yaml:"scopes"`
}

type Scope struct {
	By   string `yaml:"by"`
	Cost int    `yaml:"cost"`

	// Token bucket
	Capacity   int     `yaml:"capacity"`
	RefillRate float64 `yaml:"refill_rate"`

	// Sliding window
	Limit  int    `yaml:"limit"`
	Window string `yaml:"window"`
}

func (s Scope) EffectiveCost() int {
	if s.Cost == 0 {
		return 1
	}
	return s.Cost
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
