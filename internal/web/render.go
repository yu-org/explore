package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/yu-org/yu/config"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// pages are parsed once at startup: layout + shared partials + the page body.
type renderer struct {
	pages    map[string]*template.Template
	partials *template.Template
}

var funcs = template.FuncMap{
	"shortHash":  shortHash,
	"timeAgo":    timeAgo,
	"formatTime": formatTime,
	"comma":      comma,
	"prettyJSON": prettyJSON,
	"percent":    percent,
	"netClass":   netClass,
	"round2":     func(f float64) string { return fmt.Sprintf("%.2f", f) },
	"round1":     func(f float64) string { return fmt.Sprintf("%.1f", f) },
}

func newRenderer() (*renderer, error) {
	pageNames := []string{
		"home.html", "blocks.html", "block.html", "txs.html", "tx.html",
		"address.html", "tripods.html", "error.html",
	}
	r := &renderer{pages: make(map[string]*template.Template, len(pageNames))}

	for _, name := range pageNames {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS,
			"templates/layout.html",
			"templates/partials/*.html",
			"templates/"+name,
		)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		r.pages[name] = t
	}

	// Partials are also rendered standalone, for live updates over SSE.
	p, err := template.New("partials").Funcs(funcs).ParseFS(templateFS, "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse partials: %w", err)
	}
	r.partials = p
	return r, nil
}

// render writes a full page. It buffers first so a template error produces an
// error page instead of a half-written response.
func (r *renderer) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := r.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		http.Error(w, "render "+page+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderPartial renders a named partial to a string, for SSE payloads.
func (r *renderer) renderPartial(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := r.partials.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// --- template helpers ---

func shortHash(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	// Keep the 0x prefix visible and cut from the tail.
	return s[:n] + "…"
}

func formatTime(ts int64) string {
	if ts <= 0 {
		return "—"
	}
	return time.Unix(ts, 0).Format("2006-01-02 15:04:05 -07:00")
}

func timeAgo(ts int64) string {
	if ts <= 0 {
		return "—"
	}
	d := time.Since(time.Unix(ts, 0))
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// comma formats an integer with thousands separators.
func comma(v any) string {
	var n int64
	switch t := v.(type) {
	case int:
		n = int64(t)
	case int64:
		n = t
	case uint64:
		if t > math.MaxInt64 {
			return fmt.Sprint(t)
		}
		n = int64(t)
	default:
		return fmt.Sprint(v)
	}

	s := fmt.Sprint(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")

	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// prettyJSON indents call params when they are JSON, and returns them
// unchanged when they are not — tripods are free to use any encoding.
func prettyJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return s
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return s
	}
	return string(out)
}

// netClass maps chain_spec.network onto the handful of CSS classes the badge
// has colours for. The value is free-form in the config file, so anything
// yu does not name gets the neutral class rather than a made-up selector.
func netClass(network string) string {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case config.Mainnet:
		return "mainnet"
	case config.Testnet:
		return "testnet"
	case config.Devnet:
		return "devnet"
	default:
		return "other"
	}
}

func percent(used, limit uint64) string {
	if limit == 0 {
		return "0"
	}
	return fmt.Sprintf("%.2f", float64(used)/float64(limit)*100)
}
