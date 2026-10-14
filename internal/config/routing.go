package config

import (
	"fmt"
	"strings"
)

// Tiers a ticket's complexity maps to, smallest first. The architect sizes
// every ticket S, M or L; criticality and struggling bump it up a tier.
var Tiers = []string{"S", "M", "L"}

// ModelRouting maps a role to the model for each tier (S, M, L). Models
// are anything Claude Code's --model accepts: an alias such as "sonnet" or
// a full model ID.
type ModelRouting map[string][3]string

// DefaultModelRouting starts cheap and spends more only where it pays:
// the architect always plans with the strongest model; developers start
// on the model for the ticket's planned size and step up when they
// struggle; reviewers and testers are routed by the size of the change
// actually made, so simple work is checked by the cheapest model.
func DefaultModelRouting() ModelRouting {
	return ModelRouting{
		"architect": {"opus", "opus", "opus"},
		"developer": {"haiku", "sonnet", "opus"},
		"reviewer":  {"haiku", "sonnet", "opus"},
		"tester":    {"haiku", "sonnet", "sonnet"},
	}
}

// ParseModelRouting applies MODEL_ROUTING overrides to the defaults. The
// spec is a comma-separated list of role=model (every tier) or
// role:tier=model entries, e.g. "developer:S=haiku,reviewer=opus".
func ParseModelRouting(spec string) (ModelRouting, error) {
	r := DefaultModelRouting()
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		lhs, model, ok := strings.Cut(entry, "=")
		model = strings.TrimSpace(model)
		if !ok || model == "" {
			return nil, fmt.Errorf("%q: want role=model or role:tier=model", entry)
		}
		role, tier, hasTier := strings.Cut(strings.ToLower(strings.TrimSpace(lhs)), ":")
		models, known := r[role]
		if !known {
			return nil, fmt.Errorf("%q: unknown role %q (architect, developer, reviewer, tester)", entry, role)
		}
		if !hasTier {
			r[role] = [3]string{model, model, model}
			continue
		}
		i := tierIndex(strings.ToUpper(tier))
		if i < 0 {
			return nil, fmt.Errorf("%q: unknown tier %q (S, M, L)", entry, tier)
		}
		models[i] = model
		r[role] = models
	}
	return r, nil
}

func tierIndex(t string) int {
	for i, x := range Tiers {
		if x == t {
			return i
		}
	}
	return -1
}
