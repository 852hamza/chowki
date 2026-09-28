package web

import (
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/852hamza/chowki/internal/store"
)

const (
	// pageSize is how many requests the Requests page lists at a time.
	pageSize = 50
	// csvLimit is the most requests one CSV download holds, and csvBatch
	// how many it reads from the database at a time.
	csvLimit = 10000
	csvBatch = 1000
	// modelDays is how far back the Requests page looks for the models to
	// offer in its filter.
	modelDays = 90
)

// requestsPage is the Requests page, formatted already.
type requestsPage struct {
	CSRF, Tab             string
	Keys, Models, Results []option
	// Provider is the provider that the list is limited to, from a link of
	// the Providers card, and ClearProvider the list without that limit.
	Provider, ClearProvider string
	Rows                    []requestRow
	// Older links to the next page, and CSV to the list as a file.
	Older, CSV string
	Filtered   bool
}

type option struct {
	Value, Label string
	Selected     bool
}

type requestRow struct {
	Time, Key, KeyDetail, Model, Provider   string
	Status, StatusKind, Error               string
	Latency, Tokens, Cost, Cache, Redaction string
}

// requestQuery is what the Requests page lists: store.RequestFilter, as
// the page's links name it.
type requestQuery struct {
	key, model, provider, status string
	before                       *store.RequestCursor
}

func parseRequestQuery(q url.Values) requestQuery {
	rq := requestQuery{key: q.Get("key"), model: q.Get("model"), provider: q.Get("provider"),
		status: q.Get("status")}
	if rq.status != store.StatusFailed && rq.status != store.StatusSucceeded {
		rq.status = ""
	}
	// before is the time in milliseconds and the ID of the last request on
	// the page before.
	if ms, id, ok := strings.Cut(q.Get("before"), "."); ok {
		if n, err := strconv.ParseInt(ms, 10, 64); err == nil && id != "" {
			rq.before = &store.RequestCursor{Time: time.UnixMilli(n).UTC(), ID: id}
		}
	}
	return rq
}

// values returns the query of a link to rq, leaving out what's unset.
func (rq requestQuery) values() url.Values {
	q := url.Values{}
	for name, v := range map[string]string{"key": rq.key, "model": rq.model, "provider": rq.provider,
		"status": rq.status} {
		if v != "" {
			q.Set(name, v)
		}
	}
	if rq.before != nil {
		q.Set("before", strconv.FormatInt(rq.before.Time.UnixMilli(), 10)+"."+rq.before.ID)
	}
	return q
}

func (rq requestQuery) link(path string) string {
	if q := rq.values(); len(q) > 0 {
		return path + "?" + q.Encode()
	}
	return path
}

// filter turns rq into a store filter. A key or model that isn't known
// matches no request.
func (u *UI) filter(ctx context.Context, rq requestQuery, limit int) (store.RequestFilter, error) {
	f := store.RequestFilter{Provider: rq.provider, Status: rq.status, Before: rq.before, Limit: limit}
	if rq.key != "" {
		k, err := u.Store.KeyByPrefix(ctx, rq.key)
		switch {
		case err == nil:
			f.KeyID = k.ID
		case errors.Is(err, store.ErrNotFound):
			f.KeyID = -1
		default:
			return f, err
		}
	}
	if rq.model != "" {
		// A model of its own may hold slashes, as in openrouter/meta-llama/x.
		provider, model, ok := strings.Cut(rq.model, "/")
		if !ok || provider == "" || model == "" || rq.provider != "" && rq.provider != provider {
			provider, model = "", "\x00" // matches nothing
		}
		f.Provider, f.Model = provider, model
	}
	return f, nil
}

func (u *UI) requests(w http.ResponseWriter, r *http.Request) {
	if _, ok := u.signedIn(r); !ok {
		http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
		return
	}
	p, err := u.buildRequests(r.Context(), parseRequestQuery(r.URL.Query()))
	if err != nil {
		u.Logger.Error("dashboard requests", "error", err)
		http.Error(w, "The list of requests couldn't be built; the gateway's log has the details.",
			http.StatusInternalServerError)
		return
	}
	p.CSRF = csrfToken(w, r)
	u.render(w, http.StatusOK, "requests.html", p)
}

func (u *UI) buildRequests(ctx context.Context, rq requestQuery) (*requestsPage, error) {
	keys, err := u.Store.ListKeys(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]store.Key{}
	p := &requestsPage{Tab: "requests", Provider: rq.provider,
		Filtered: rq.key != "" || rq.model != "" || rq.provider != "" || rq.status != ""}
	p.Keys = append(p.Keys, option{"", "All keys", rq.key == ""})
	slices.SortFunc(keys, func(a, b store.Key) int { return cmp.Or(strings.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
	for _, k := range keys {
		byID[k.ID] = k
		label := k.Name + " · " + k.Prefix
		if k.Revoked() {
			label += " (revoked)"
		}
		p.Keys = append(p.Keys, option{k.Prefix, label, k.Prefix == rq.key})
	}

	now := u.now().UTC()
	models, err := u.Store.Breakdown(ctx, store.ByModel, now.AddDate(0, 0, -modelDays), now)
	if err != nil {
		return nil, err
	}
	p.Models = append(p.Models, option{"", "All models", rq.model == ""})
	ids := []string{}
	for _, g := range models {
		if g.ID != "" {
			ids = append(ids, g.ID)
		}
	}
	if rq.model != "" && !slices.Contains(ids, rq.model) {
		ids = append(ids, rq.model) // older than the list, but asked for
	}
	slices.Sort(ids)
	for _, id := range ids {
		p.Models = append(p.Models, option{id, id, id == rq.model})
	}
	for _, s := range []option{{"", "All results", false}, {store.StatusFailed, "Failed", false},
		{store.StatusSucceeded, "Succeeded", false}} {
		s.Selected = s.Value == rq.status
		p.Results = append(p.Results, s)
	}
	if rq.provider != "" {
		all := rq
		all.provider, all.before = "", nil
		p.ClearProvider = all.link("/ui/requests")
	}

	f, err := u.filter(ctx, rq, pageSize)
	if err != nil {
		return nil, err
	}
	rs, err := u.Store.ListRequests(ctx, f)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		p.Rows = append(p.Rows, formatRequest(r, byID))
	}
	if len(rs) == pageSize {
		next := rq
		last := rs[len(rs)-1]
		next.before = &store.RequestCursor{Time: last.Time, ID: last.ID}
		p.Older = next.link("/ui/requests")
	}
	first := rq
	first.before = nil
	p.CSV = first.link("/ui/requests.csv")
	return p, nil
}

func formatRequest(r store.Request, keys map[int64]store.Key) requestRow {
	row := requestRow{Time: r.Time.Format(time.DateTime), Latency: duration(r.Latency),
		Status: strconv.Itoa(r.Status), StatusKind: "ok", Tokens: "—", Cost: "—", Cache: cmp.Or(r.CacheStatus, "—"),
		Redaction: "—"}
	if k, ok := keys[r.KeyID]; ok {
		row.Key, row.KeyDetail = k.Name, k.Prefix
	} else {
		row.Key, row.KeyDetail = "No valid key", "rejected by authentication"
	}
	switch {
	case r.Model != "":
		row.Model, row.Provider = r.Model, r.Provider
	default:
		row.Model = "No model"
	}
	if r.Status >= 400 || r.ErrorType != "" {
		row.StatusKind, row.Error = "critical", r.ErrorType
	}
	if r.Tokens != nil {
		row.Tokens = count(r.Tokens.Input) + " in · " + count(r.Tokens.Output) + " out"
	}
	switch {
	case r.CostUSD != nil:
		row.Cost = preciseUSD(*r.CostUSD)
	case r.Tokens != nil:
		row.Cost = "unpriced"
	}
	if len(r.Redactions) > 0 {
		var parts []string
		for _, t := range slices.Sorted(maps.Keys(r.Redactions)) {
			parts = append(parts, fmt.Sprintf("%s %d", t, r.Redactions[t]))
		}
		row.Redaction = strings.Join(parts, ", ")
	}
	return row
}

// duration formats a latency to about three digits: 83 ms, 1.25 s, 18.3 s.
func duration(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	switch {
	case ms < 1000:
		return strconv.FormatFloat(math.Round(ms), 'f', 0, 64) + " ms"
	case ms < 10000:
		return strconv.FormatFloat(ms/1000, 'f', 2, 64) + " s"
	}
	return strconv.FormatFloat(ms/1000, 'f', 1, 64) + " s"
}

// preciseUSD formats the cost of one request, which is often a fraction of
// a cent, to two significant digits below a cent.
func preciseUSD(v float64) string {
	if v <= 0 || v >= 0.00995 { // from 0.00995, it rounds to a cent: $0.01, not $0.0100
		return usd(v)
	}
	digits := min(int(-math.Floor(math.Log10(v)))+1, 8)
	return "$" + strconv.FormatFloat(v, 'f', digits, 64)
}

// requestsCSV sends the requests that match the page's filters as CSV, the
// newest first, up to csvLimit.
func (u *UI) requestsCSV(w http.ResponseWriter, r *http.Request) {
	if _, ok := u.signedIn(r); !ok {
		http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
		return
	}
	ctx := r.Context()
	rq := parseRequestQuery(r.URL.Query())
	rq.before = nil
	f, err := u.filter(ctx, rq, csvBatch)
	if err == nil {
		var keys []store.Key
		if keys, err = u.Store.ListKeys(ctx); err == nil {
			err = u.writeCSV(ctx, w, f, keys)
		}
	}
	if err != nil {
		u.Logger.Error("dashboard requests csv", "error", err)
	}
}

func (u *UI) writeCSV(ctx context.Context, w http.ResponseWriter, f store.RequestFilter, keys []store.Key) error {
	byID := map[int64]store.Key{}
	for _, k := range keys {
		byID[k.ID] = k
	}
	// The first page is read before any byte is sent, so that an error can
	// still become a 500.
	rs, err := u.Store.ListRequests(ctx, f)
	if err != nil {
		http.Error(w, "The requests couldn't be read; the gateway's log has the details.",
			http.StatusInternalServerError)
		return err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="chowki-requests-`+u.now().UTC().Format("20060102-150405")+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"time", "request_id", "key_name", "key_prefix", "provider", "model", "endpoint", "stream",
		"status", "error", "latency_ms", "ttfb_ms", "input_tokens", "output_tokens", "cache_read_tokens",
		"cache_write_tokens", "reasoning_tokens", "cost_usd", "savings_usd", "savings_method", "cache",
		"redactions"})
	written := 0
	for {
		for _, r := range rs {
			_ = cw.Write(csvRecord(r, byID))
		}
		written += len(rs)
		if len(rs) < f.Limit || written >= csvLimit {
			break
		}
		last := rs[len(rs)-1]
		f.Before = &store.RequestCursor{Time: last.Time, ID: last.ID}
		f.Limit = min(csvBatch, csvLimit-written)
		if rs, err = u.Store.ListRequests(ctx, f); err != nil {
			cw.Flush()
			return err // the file ends early; the log says why
		}
	}
	cw.Flush()
	return cw.Error()
}

func csvRecord(r store.Request, keys map[int64]store.Key) []string {
	k := keys[r.KeyID]
	num := func(n int64) string { return strconv.FormatInt(n, 10) }
	var tokens [5]string
	if t := r.Tokens; t != nil {
		tokens = [5]string{num(t.Input), num(t.Output), num(t.CacheRead), num(t.CacheWrite), num(t.Reasoning)}
	}
	cost := ""
	if r.CostUSD != nil {
		cost = strconv.FormatFloat(*r.CostUSD, 'f', -1, 64)
	}
	var redactions []string
	for _, t := range slices.Sorted(maps.Keys(r.Redactions)) {
		redactions = append(redactions, fmt.Sprintf("%s:%d", t, r.Redactions[t]))
	}
	return []string{r.Time.Format(time.RFC3339Nano), cell(r.ID), cell(k.Name), cell(k.Prefix), cell(r.Provider),
		cell(r.Model), cell(r.Endpoint), strconv.FormatBool(r.Stream), strconv.Itoa(r.Status), cell(r.ErrorType),
		num(r.Latency.Milliseconds()), num(r.TTFB.Milliseconds()), tokens[0], tokens[1], tokens[2], tokens[3],
		tokens[4], cost, strconv.FormatFloat(r.SavingsUSD, 'f', -1, 64), cell(r.SavingsMethod), cell(r.CacheStatus),
		cell(strings.Join(redactions, " "))}
}

// cell keeps a spreadsheet from running a text as a formula: clients name
// models, and a model such as =HYPERLINK(...) would otherwise become one.
// It prefixes such text with an apostrophe, as OWASP recommends.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
