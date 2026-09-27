package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

// API serves the admin JSON API under /admin/v1/. Every request needs an
// admin token in the Authorization: Bearer header.
type API struct {
	Store  store.Store
	Logger *slog.Logger
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// maxBody limits the body of an admin request.
const maxBody = 1 << 20

// Handler returns the handler of the admin API.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/v1/summary", a.summary)
	mux.HandleFunc("GET /admin/v1/breakdown", a.breakdown)
	mux.HandleFunc("GET /admin/v1/keys", a.listKeys)
	mux.HandleFunc("POST /admin/v1/keys", a.createKey)
	mux.HandleFunc("PATCH /admin/v1/keys/{prefix}", a.updateKey)
	mux.HandleFunc("DELETE /admin/v1/keys/{prefix}", a.revokeKey)
	mux.HandleFunc("GET /admin/v1/projects", a.listProjects)
	mux.HandleFunc("PATCH /admin/v1/projects/{name}", a.updateProject)
	mux.HandleFunc("GET /admin/v1/requests", a.requests)
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", fmt.Sprintf("%s %s isn't an admin endpoint.", r.Method, r.URL.Path))
	})
	return a.authenticate(mux)
}

type tokenKey struct{}

// authenticate lets through requests with a valid admin token, and puts the
// token in their context for the audit log.
func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "missing_admin_token",
				"Send an admin token in the Authorization: Bearer header; chowki admin create makes one.")
			return
		}
		t, err := auth.VerifyAdmin(r.Context(), a.Store, strings.TrimSpace(token))
		switch {
		case errors.Is(err, auth.ErrInvalid), errors.Is(err, auth.ErrRevoked):
			a.Logger.Warn("admin request rejected", "path", r.URL.Path, "reason", err)
			writeError(w, http.StatusUnauthorized, "invalid_admin_token", "The admin token is invalid or revoked.")
			return
		case err != nil:
			a.internalError(w, "verify admin token", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, t)))
	})
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// actor names the admin token of a request in the audit log.
func actor(r *http.Request) string {
	t, _ := r.Context().Value(tokenKey{}).(store.AdminToken)
	return "admin:" + t.Prefix
}

func (a *API) audit(r *http.Request, action, target string, details map[string]string) error {
	return a.Store.AddAudit(r.Context(), store.AuditEvent{Time: a.now().UTC(), Actor: actor(r), Action: action,
		Target: target, Details: details})
}

// dayRange reads the from and to query parameters: dates such as
// 2026-09-01, where to includes its day. Reports count whole days in UTC.
// The range defaults to the current month to date. It returns the range as
// [from, to).
func (a *API) dayRange(r *http.Request) (from, to time.Time, err error) {
	now := a.now().UTC()
	from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	for name, t := range map[string]*time.Time{"from": &from, "to": &to} {
		v := r.URL.Query().Get(name)
		if v == "" {
			continue
		}
		d, e := time.Parse(time.DateOnly, v)
		if e != nil {
			return from, to, fmt.Errorf("%s must be a date such as 2026-09-01", name)
		}
		if name == "to" {
			d = d.AddDate(0, 0, 1)
		}
		*t = d
	}
	if !from.Before(to) {
		return from, to, errors.New("from must be on or before to")
	}
	return from, to, nil
}

// days formats [from, to) as the dates of its first and last days.
func days(from, to time.Time) (string, string) {
	return from.Format(time.DateOnly), to.AddDate(0, 0, -1).Format(time.DateOnly)
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	from, to, err := a.dayRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	t, err := a.Store.Totals(r.Context(), from, to)
	if err != nil {
		a.internalError(w, "sum requests", err)
		return
	}
	hitRate := 0.0
	if n := t.CacheHits + t.CacheMisses; n > 0 {
		hitRate = float64(t.CacheHits) / float64(n)
	}
	first, last := days(from, to)
	writeJSON(w, http.StatusOK, map[string]any{
		"from": first, "to": last, "requests": t.Requests, "errors": t.Errors, "unpriced": t.Unpriced,
		"cost_usd": t.CostUSD, "savings_usd": t.SavingsUSD, "redactions": t.Redactions,
		"tokens": map[string]int64{"input": t.Tokens.Input, "output": t.Tokens.Output, "cache_read": t.Tokens.CacheRead,
			"cache_write": t.Tokens.CacheWrite, "reasoning": t.Tokens.Reasoning},
		"cache": map[string]any{"hits": t.CacheHits, "misses": t.CacheMisses, "hit_rate": hitRate},
	})
}

type group struct {
	ID           string  `json:"id"`
	Label        string  `json:"label,omitempty"`
	Requests     int64   `json:"requests"`
	CostUSD      float64 `json:"cost_usd"`
	SavingsUSD   float64 `json:"savings_usd"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
}

func (a *API) breakdown(w http.ResponseWriter, r *http.Request) {
	from, to, err := a.dayRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	by := r.URL.Query().Get("by")
	if by == "" {
		by = store.ByKey
	}
	if by != store.ByKey && by != store.ByModel && by != store.ByDay {
		writeError(w, http.StatusBadRequest, "invalid_request", "by must be key, model or day")
		return
	}
	gs, err := a.Store.Breakdown(r.Context(), by, from, to)
	if err != nil {
		a.internalError(w, "break down requests", err)
		return
	}
	out := make([]group, 0, len(gs))
	for _, g := range gs {
		out = append(out, group{ID: g.ID, Label: g.Label, Requests: g.Requests, CostUSD: g.CostUSD,
			SavingsUSD: g.SavingsUSD, InputTokens: g.InputTokens, OutputTokens: g.OutputTokens})
	}
	first, last := days(from, to)
	writeJSON(w, http.StatusOK, map[string]any{"by": by, "from": first, "to": last, "groups": out})
}

// key is a virtual key as the admin API shows it: never the key itself.
type key struct {
	Prefix           string     `json:"prefix"`
	Name             string     `json:"name"`
	Project          string     `json:"project"`
	CreatedAt        time.Time  `json:"created_at"`
	RevokedAt        *time.Time `json:"revoked_at"`
	BudgetUSD        float64    `json:"budget_usd"`
	ProjectBudgetUSD float64    `json:"project_budget_usd"`
	RPM              int64      `json:"rpm"`
	TPM              int64      `json:"tpm"`
	Cache            string     `json:"cache"`
	Redaction        string     `json:"redaction"`
	Models           []string   `json:"models"`
	// SpentUSD is what the key spent this month (UTC).
	SpentUSD float64 `json:"spent_usd"`
}

func toKey(k store.Key, spent float64) key {
	out := key{Prefix: k.Prefix, Name: k.Name, Project: k.Project, CreatedAt: k.CreatedAt, BudgetUSD: k.BudgetUSD,
		ProjectBudgetUSD: k.ProjectBudgetUSD, RPM: k.RPM, TPM: k.TPM, Cache: orDefault(k.CacheMode),
		Redaction: orDefault(k.RedactionMode), Models: k.AllowedModels, SpentUSD: spent}
	if k.Revoked() {
		out.RevokedAt = &k.RevokedAt
	}
	if out.Models == nil {
		out.Models = []string{}
	}
	return out
}

func orDefault(mode string) string {
	if mode == "" {
		return "default"
	}
	return mode
}

// spentThisMonth returns what each key spent this month, by key ID.
func (a *API) spentThisMonth(ctx context.Context) (map[int64]float64, error) {
	spend, err := a.Store.SpendByKey(ctx, store.Period(a.now()))
	if err != nil {
		return nil, err
	}
	out := map[int64]float64{}
	for _, s := range spend {
		out[s.KeyID] = s.USD
	}
	return out, nil
}

func (a *API) listKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.Store.ListKeys(r.Context())
	if err != nil {
		a.internalError(w, "list keys", err)
		return
	}
	spent, err := a.spentThisMonth(r.Context())
	if err != nil {
		a.internalError(w, "read spend", err)
		return
	}
	out := make([]key, 0, len(keys))
	for _, k := range keys {
		out = append(out, toKey(k, spent[k.ID]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

// settings are the settings of a key in a request; absent fields keep
// theirs.
type settings struct {
	BudgetUSD *float64  `json:"budget_usd"`
	RPM       *int64    `json:"rpm"`
	TPM       *int64    `json:"tpm"`
	Cache     *string   `json:"cache"`
	Redaction *string   `json:"redaction"`
	Models    *[]string `json:"models"`
}

func (s settings) update() store.KeyUpdate {
	u := store.KeyUpdate{BudgetUSD: s.BudgetUSD, RPM: s.RPM, TPM: s.TPM, AllowedModels: s.Models}
	for _, m := range []struct {
		in  *string
		out **string
	}{{s.Cache, &u.CacheMode}, {s.Redaction, &u.RedactionMode}} {
		if m.in != nil {
			v := *m.in
			if v == "default" {
				v = ""
			}
			*m.out = &v
		}
	}
	return u
}

func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Project string `json:"project"`
		settings
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Project == "" {
		req.Project = "default"
	}
	for label, v := range map[string]string{"name": req.Name, "project": req.Project} {
		if err := store.CheckName(v); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", label+" "+err.Error())
			return
		}
	}
	u := req.update()
	if err := u.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	k := store.Key{Name: req.Name}
	if u.BudgetUSD != nil {
		k.BudgetUSD = *u.BudgetUSD
	}
	if u.RPM != nil {
		k.RPM = *u.RPM
	}
	if u.TPM != nil {
		k.TPM = *u.TPM
	}
	if u.CacheMode != nil {
		k.CacheMode = *u.CacheMode
	}
	if u.RedactionMode != nil {
		k.RedactionMode = *u.RedactionMode
	}
	if u.AllowedModels != nil {
		k.AllowedModels = *u.AllowedModels
	}
	secret, stored, err := auth.Create(r.Context(), a.Store, req.Project, k, a.now().UTC())
	if err != nil {
		a.internalError(w, "create key", err)
		return
	}
	if err := a.audit(r, "key.create", stored.Prefix, map[string]string{"name": stored.Name,
		"project": stored.Project}); err != nil {
		a.internalError(w, "audit", err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		Key string `json:"key"`
		key
	}{secret, toKey(stored, 0)})
}

func (a *API) updateKey(w http.ResponseWriter, r *http.Request) {
	var req settings
	if !decode(w, r, &req) {
		return
	}
	u := req.update()
	if err := u.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	prefix := auth.PrefixOf(r.PathValue("prefix"))
	k, err := a.Store.UpdateKey(r.Context(), prefix, u)
	if a.keyError(w, prefix, err) {
		return
	}
	if err := a.audit(r, "key.update", k.Prefix, map[string]string{"name": k.Name, "project": k.Project}); err != nil {
		a.internalError(w, "audit", err)
		return
	}
	spent, err := a.spentThisMonth(r.Context())
	if err != nil {
		a.internalError(w, "read spend", err)
		return
	}
	writeJSON(w, http.StatusOK, toKey(k, spent[k.ID]))
}

func (a *API) revokeKey(w http.ResponseWriter, r *http.Request) {
	prefix := auth.PrefixOf(r.PathValue("prefix"))
	now := a.now().UTC()
	k, err := a.Store.RevokeKey(r.Context(), prefix, now)
	if a.keyError(w, prefix, err) {
		return
	}
	if k.RevokedAt.Equal(now.Truncate(time.Millisecond)) {
		if err := a.audit(r, "key.revoke", k.Prefix, map[string]string{"name": k.Name, "project": k.Project}); err != nil {
			a.internalError(w, "audit", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toKey(k, 0))
}

// keyError answers err from a key lookup, and reports whether there was one.
func (a *API) keyError(w http.ResponseWriter, prefix string, err error) bool {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "key_not_found", fmt.Sprintf("No virtual key has the prefix %q.", prefix))
	case err != nil:
		a.internalError(w, "update key", err)
	default:
		return false
	}
	return true
}

type project struct {
	Name       string  `json:"name"`
	BudgetUSD  float64 `json:"budget_usd"`
	SpentUSD   float64 `json:"spent_usd"`
	ActiveKeys int     `json:"active_keys"`
}

func (a *API) listProjects(w http.ResponseWriter, r *http.Request) {
	out, err := a.projects(r.Context())
	if err != nil {
		a.internalError(w, "list projects", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

// projects returns every project with this month's spend of its keys.
func (a *API) projects(ctx context.Context) ([]project, error) {
	projects, err := a.Store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := a.Store.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	spent, err := a.spentThisMonth(ctx)
	if err != nil {
		return nil, err
	}
	byProject := map[int64]*project{}
	out := make([]project, len(projects))
	for i, p := range projects {
		out[i] = project{Name: p.Name, BudgetUSD: p.BudgetUSD}
		byProject[p.ID] = &out[i]
	}
	for _, k := range keys {
		if p := byProject[k.ProjectID]; p != nil {
			p.SpentUSD += spent[k.ID]
			if !k.Revoked() {
				p.ActiveKeys++
			}
		}
	}
	return out, nil
}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BudgetUSD *float64 `json:"budget_usd"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := (store.KeyUpdate{BudgetUSD: req.BudgetUSD}).Validate(); err != nil || req.BudgetUSD == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "budget_usd must be an amount of 0 or more")
		return
	}
	name := r.PathValue("name")
	p, err := a.Store.UpdateProject(r.Context(), name, store.ProjectUpdate{BudgetUSD: req.BudgetUSD})
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "project_not_found", fmt.Sprintf("No project is named %q.", name))
		return
	case err != nil:
		a.internalError(w, "update project", err)
		return
	}
	if err := a.audit(r, "project.update", p.Name, map[string]string{
		"monthly_budget_usd": strconv.FormatFloat(p.BudgetUSD, 'f', -1, 64)}); err != nil {
		a.internalError(w, "audit", err)
		return
	}
	all, err := a.projects(r.Context())
	if err != nil {
		a.internalError(w, "list projects", err)
		return
	}
	for _, q := range all {
		if q.Name == p.Name {
			writeJSON(w, http.StatusOK, q)
			return
		}
	}
}

type tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
	Reasoning  int64 `json:"reasoning"`
}

// request is a request record as the admin API shows it: metadata only.
type request struct {
	ID            string         `json:"id"`
	Time          time.Time      `json:"time"`
	KeyID         int64          `json:"key_id"`
	ProjectID     int64          `json:"project_id"`
	Family        string         `json:"family"`
	Endpoint      string         `json:"endpoint"`
	Provider      string         `json:"provider"`
	Model         string         `json:"model"`
	Stream        bool           `json:"stream"`
	Status        int            `json:"status"`
	Error         string         `json:"error"`
	LatencyMS     int64          `json:"latency_ms"`
	TTFBMS        int64          `json:"ttfb_ms"`
	Tokens        *tokens        `json:"tokens"`
	CostUSD       *float64       `json:"cost_usd"`
	SavingsUSD    float64        `json:"savings_usd"`
	SavingsMethod string         `json:"savings_method"`
	Cache         string         `json:"cache"`
	Redactions    map[string]int `json:"redactions"`
}

func (a *API) requests(w http.ResponseWriter, r *http.Request) {
	limit, before := 100, a.now()
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be a number from 1 to 1000")
			return
		}
		limit = n
	}
	if v := r.URL.Query().Get("before"); v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "before must be a time such as 2026-09-27T10:00:00Z")
			return
		}
		before = t
	}
	rs, err := a.Store.RecentRequests(r.Context(), before, limit)
	if err != nil {
		a.internalError(w, "read requests", err)
		return
	}
	out := make([]request, 0, len(rs))
	for _, q := range rs {
		o := request{ID: q.ID, Time: q.Time, KeyID: q.KeyID, ProjectID: q.ProjectID, Family: q.APIFamily,
			Endpoint: q.Endpoint, Provider: q.Provider, Model: q.Model, Stream: q.Stream, Status: q.Status,
			Error: q.ErrorType, LatencyMS: q.Latency.Milliseconds(), TTFBMS: q.TTFB.Milliseconds(),
			CostUSD: q.CostUSD, SavingsUSD: q.SavingsUSD, SavingsMethod: q.SavingsMethod, Cache: q.CacheStatus,
			Redactions: q.Redactions}
		if t := q.Tokens; t != nil {
			o.Tokens = &tokens{t.Input, t.Output, t.CacheRead, t.CacheWrite, t.Reasoning}
		}
		if o.Redactions == nil {
			o.Redactions = map[string]int{}
		}
		out = append(out, o)
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// decode reads a JSON body into v, and answers a 400 when it can't.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body: "+err.Error()+".")
		return false
	}
	return true
}

func (a *API) internalError(w http.ResponseWriter, what string, err error) {
	a.Logger.Error("admin API", "operation", what, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "The gateway failed; its log has the details.")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // the client may be gone; nothing to do then
}
