package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/pdf"
)

// funcs are static across requests. T, THTML and CSRF are registered as
// neutral stubs so parsing succeeds; every render swaps in locale/token-
// bound overrides (see bindLang), so the stubs themselves never reach users.
var funcs = template.FuncMap{
	"T":         func(key string, args ...any) string { return "!" + key },
	"THTML":     func(key string, args ...any) template.HTML { return template.HTML("!" + key) },
	"CSRF":      func() string { return "" },
	"brl":       pdf.FormatBRL,
	"dtbr":      func(t time.Time) string { return t.Format("02/01/2006") },
	"dtin":      func(t time.Time) string { return t.Format("2006-01-02") },
	"hasPrefix": strings.HasPrefix,
}

// bindLang clones a parsed template and overrides its T function with one
// bound to the request locale, plus CSRF to return this request's token.
// Cloning keeps the shared parsed templates immutable under concurrent
// requests; functions resolve at exec time, so the overrides apply to this
// render only. CSRF being a function (not pageData) lets any block — even
// the content block, whose dot is the page payload — reach the token.
func bindLang(tpl *template.Template, lang i18n.Lang, csrf string) (*template.Template, error) {
	clone, err := tpl.Clone()
	if err != nil {
		return nil, err
	}
	return clone.Funcs(template.FuncMap{
		"T":     func(key string, args ...any) string { return i18n.T(lang, key, args...) },
		"THTML": func(key string, args ...any) template.HTML { return template.HTML(i18n.T(lang, key, args...)) },
		"CSRF":  func() string { return csrf },
	}), nil
}

// pageData is the payload handed to layout.html; Data carries the
// page-specific view struct consumed by the "content" block. Authed and
// CSRFToken are request-derived chrome filled by Handlers.renderPage.
type pageData struct {
	Title     string
	Lang      i18n.Lang
	Authed    bool
	CSRFToken string
	Data      any
}

type renderer struct {
	pages map[string]*template.Template
	frags map[string]*template.Template
}

func newRenderer(fsys fs.FS) (*renderer, error) {
	r := &renderer{pages: map[string]*template.Template{}, frags: map[string]*template.Template{}}

	pageFiles, err := fs.Glob(fsys, "templates/pages/*.html")
	if err != nil {
		return nil, fmt.Errorf("glob pages: %w", err)
	}
	for _, f := range pageFiles {
		base := path.Base(f)
		tpl, err := template.New(base).Funcs(funcs).ParseFS(fsys,
			"templates/layout.html", "templates/partials.html", f)
		if err != nil {
			return nil, fmt.Errorf("parse page %s: %w", base, err)
		}
		r.pages[base] = tpl
	}

	fragFiles, err := fs.Glob(fsys, "templates/fragments/*.html")
	if err != nil {
		return nil, fmt.Errorf("glob fragments: %w", err)
	}
	for _, f := range fragFiles {
		base := path.Base(f)
		tpl, err := template.New(base).Funcs(funcs).ParseFS(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("parse fragment %s: %w", base, err)
		}
		r.frags[base] = tpl
	}

	if len(r.pages) == 0 {
		return nil, fmt.Errorf("no page templates found under templates/pages/")
	}
	return r, nil
}

// renderPage executes a full-layout page. Rendering failures are logged and
// surface as a plain-text 500 instead of a half-written document. pg
// carries the fully assembled chrome; Handlers.renderPage is the entry
// point page handlers use.
func (rnd *renderer) renderPage(w http.ResponseWriter, status int, name string, pg pageData) {
	tpl, ok := rnd.pages[name]
	if !ok {
		log.Printf("web: page template %q not registered", name)
		http.Error(w, i18n.T(pg.Lang, "error.internal"), http.StatusInternalServerError)
		return
	}
	tpl, err := bindLang(tpl, pg.Lang, pg.CSRFToken)
	if err != nil {
		log.Printf("web: bind page %s: %v", name, err)
		http.Error(w, i18n.T(pg.Lang, "error.render_page"), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "layout.html", pg); err != nil {
		log.Printf("web: render page %s: %v", name, err)
		http.Error(w, i18n.T(pg.Lang, "error.render_page"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

// renderFragment executes an htmx partial; same loud-failure behavior as
// renderPage.
func (rnd *renderer) renderFragment(w http.ResponseWriter, name string, lang i18n.Lang, data any) {
	tpl, ok := rnd.frags[name]
	if !ok {
		log.Printf("web: fragment template %q not registered", name)
		http.Error(w, i18n.T(lang, "error.internal"), http.StatusInternalServerError)
		return
	}
	tpl, err := bindLang(tpl, lang, "")
	if err != nil {
		log.Printf("web: bind fragment %s: %v", name, err)
		http.Error(w, i18n.T(lang, "error.render_fragment"), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("web: render fragment %s: %v", name, err)
		http.Error(w, i18n.T(lang, "error.render_fragment"), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}

// renderPage renders a full-layout page on behalf of a page handler. It
// resolves the request-derived chrome — whether the visitor holds a valid
// session (drives the nav's logout button) and the CSRF token for forms
// and htmx — so individual handlers only supply title, locale and payload.
// The CSRF token is read from the request's double-submit cookie; if the
// browser dropped it mid-session we mint a fresh one (and set the cookie)
// so the page's forms still validate on submit.
func (h *Handlers) renderPage(w http.ResponseWriter, r *http.Request, status int, name, title string, lang i18n.Lang, data any) {
	authed, err := h.auth.ValidSession(r)
	if err != nil {
		// Nav chrome only: treat lookup failures as "not authed" but log.
		log.Printf("web: session check for nav: %v", err)
	}
	csrf := ""
	if c, cerr := r.Cookie("csrf_token"); cerr == nil && c.Value != "" {
		csrf = c.Value
	} else if raw, terr := h.auth.IssueCSRFToken(w); terr == nil {
		csrf = raw
	}
	h.render.renderPage(w, status, name, pageData{
		Title:     title,
		Lang:      lang,
		Authed:    authed,
		CSRFToken: csrf,
		Data:      data,
	})
}
