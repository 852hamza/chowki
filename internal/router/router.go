package router

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/852hamza/chowki/internal/catalog"
	"github.com/852hamza/chowki/internal/providers"
)

const (
	// failuresToOpen is how many failures in a row make a target
	// unhealthy.
	failuresToOpen = 3
	// openFor is how long an unhealthy target is skipped.
	openFor = 30 * time.Second
)

// Target is a provider and the model that it serves.
type Target struct {
	Provider *providers.Provider
	Model    string
}

func (t Target) String() string { return t.Provider.Name + "/" + t.Model }

// Error is a request for a model that the router can't serve, with a
// stable code and a message that says what to do.
type Error struct {
	Code, Message string
}

func (e *Error) Error() string { return e.Message }

// Error codes.
const (
	CodeUnknownProvider = "unknown_provider"
	CodeWrongEndpoint   = "wrong_endpoint"
)

// Router resolves the model that a request names to the targets that can
// serve it, in the order to try them, and tracks their health.
type Router struct {
	providers map[string]*providers.Provider
	catalog   *catalog.Catalog
	aliases   map[string][]string // alias → provider/model targets, in order

	mu     sync.Mutex
	health map[string]*health // by target
}

// health counts a target's failures in a row; it's skipped until openUntil.
type health struct {
	failures  int
	openUntil time.Time
}

// New returns a router over providers ps, with the catalog and aliases,
// which map a name to provider/model targets. It checks that every target
// names a provider.
func New(ps map[string]*providers.Provider, cat *catalog.Catalog, aliases map[string][]string) (*Router, error) {
	for name, targets := range aliases {
		for _, t := range targets {
			p, model, ok := strings.Cut(t, "/")
			if _, known := ps[p]; !ok || model == "" || !known {
				return nil, fmt.Errorf("alias %s: target %q must be <provider>/<model> with a configured provider", name, t)
			}
		}
	}
	return &Router{providers: ps, catalog: cat, aliases: aliases, health: map[string]*health{}}, nil
}

// Resolve returns the targets for requested, a model name or an alias,
// whose providers speak one of the API types in accept, in the order to try
// them: healthy targets first, each group in configured order. The first
// type in accept is the endpoint's own; the others are those it reaches
// through translation.
//
// A name is resolved like this: an alias gives its targets;
// "<provider>/<model>" names one; otherwise the provider that the catalog
// lists for the model, or the only provider of the endpoint's own type.
func (r *Router) Resolve(accept []string, requested string, now time.Time) ([]Target, error) {
	var targets []Target
	if names, ok := r.aliases[requested]; ok {
		for _, name := range names {
			p, model, _ := strings.Cut(name, "/")
			if slices.Contains(accept, r.providers[p].Type) {
				targets = append(targets, Target{r.providers[p], model})
			}
		}
		if len(targets) == 0 {
			return nil, &Error{CodeWrongEndpoint, fmt.Sprintf(
				"The alias %q has no target that this endpoint can reach; send requests for it to another endpoint.",
				requested)}
		}
	} else {
		t, err := r.one(accept, requested)
		if err != nil {
			return nil, err
		}
		targets = []Target{t}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	sort.SliceStable(targets, func(i, j int) bool {
		return r.healthy(targets[i], now) && !r.healthy(targets[j], now)
	})
	return targets, nil
}

func (r *Router) one(accept []string, requested string) (Target, error) {
	wantType := accept[0]
	check := func(p *providers.Provider, model string) (Target, error) {
		if !slices.Contains(accept, p.Type) {
			return Target{}, &Error{CodeWrongEndpoint, fmt.Sprintf(
				"The provider %q speaks the %s API; send requests for it to that API's endpoint.", p.Name, p.Type)}
		}
		return Target{p, model}, nil
	}
	if name, model, ok := strings.Cut(requested, "/"); ok && model != "" {
		if p, ok := r.providers[name]; ok {
			return check(p, model)
		}
	}
	var listed []*providers.Provider
	for _, name := range r.catalog.Providers(requested) {
		if p, ok := r.providers[name]; ok && slices.Contains(accept, p.Type) {
			listed = append(listed, p)
		}
	}
	if len(listed) == 1 {
		return Target{listed[0], requested}, nil
	}
	var family []string
	for name, p := range r.providers {
		if p.Type == wantType {
			family = append(family, name)
		}
	}
	if len(family) == 1 {
		return Target{r.providers[family[0]], requested}, nil
	}
	slices.Sort(family)
	hint := "Add a provider of this API to chowki.yaml."
	if len(family) > 1 {
		hint = fmt.Sprintf("Name it as provider/model, with a provider from: %s.", strings.Join(family, ", "))
	}
	return Target{}, &Error{CodeUnknownProvider, fmt.Sprintf(
		"The gateway can't tell which provider serves the model %q. %s", requested, hint)}
}

// ModelInfo is a name that requests can use: an alias or a catalog model.
type ModelInfo struct {
	ID    string // an alias, or <provider>/<model>
	Owner string // the provider, or "chowki" for an alias
}

// Models returns the aliases with a target of one of the API types in
// accept, and the catalog's models of the configured providers of those
// types, by name.
func (r *Router) Models(accept []string) []ModelInfo {
	var out []ModelInfo
	for name, targets := range r.aliases {
		for _, t := range targets {
			if p, _, _ := strings.Cut(t, "/"); slices.Contains(accept, r.providers[p].Type) {
				out = append(out, ModelInfo{ID: name, Owner: "chowki"})
				break
			}
		}
	}
	if r.catalog != nil {
		for _, m := range r.catalog.All() {
			if p, ok := r.providers[m.Provider]; ok && slices.Contains(accept, p.Type) {
				out = append(out, ModelInfo{ID: m.Provider + "/" + m.Model, Owner: m.Provider})
			}
		}
	}
	slices.SortFunc(out, func(a, b ModelInfo) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// Report records whether a call to target t worked. Failures are the ones
// that allow a fallback: rate limits, server errors, and connection
// errors and timeouts.
func (r *Router) Report(t Target, ok bool, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := t.String()
	if ok {
		delete(r.health, key)
		return
	}
	h := r.health[key]
	if h == nil {
		h = &health{}
		r.health[key] = h
	}
	// The count stays at failuresToOpen or above, so that one more failure
	// after the pause opens the target again.
	if h.failures++; h.failures >= failuresToOpen {
		h.openUntil = now.Add(openFor)
	}
}

func (r *Router) healthy(t Target, now time.Time) bool {
	h := r.health[t.String()]
	return h == nil || !now.Before(h.openUntil)
}
