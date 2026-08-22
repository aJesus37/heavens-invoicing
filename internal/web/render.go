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

// funcs are static across requests. T is registered as a neutral stub so
// parsing succeeds; every render swaps in a locale-bound override (see
// bindLang), so the stub itself never reaches users.
var funcs = template.FuncMap{
	"T":         func(key string, args ...any) string { return "!" + key },
	"brl":       pdf.FormatBRL,
	"dtbr":      func(t time.Time) string { return t.Format("02/01/2006") },
	"dtin":      func(t time.Time) string { return t.Format("2006-01-02") },
	"hasPrefix": strings.HasPrefix,
}

// bindLang clones a parsed template and overrides its T function with one
// bound to the request locale. Cloning keeps the shared parsed templates
// immutable under concurrent requests; functions resolve at exec time, so
// the override applies to this render only.
func bindLang(tpl *template.Template, lang i18n.Lang) (*template.Template, error) {
	clone, err := tpl.Clone()
	if err != nil {
		return nil, err
	}
	return clone.Funcs(template.FuncMap{
		"T": func(key string, args ...any) string { return i18n.T(lang, key, args...) },
	}), nil
}

// pageData is the payload handed to layout.html; Data carries the
// page-specific view struct consumed by the "content" block.
type pageData struct {
	Title string
	Lang  i18n.Lang
	Data  any
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
// surface as a plain-text 500 instead of a half-written document.
func (rnd *renderer) renderPage(w http.ResponseWriter, status int, name, title string, lang i18n.Lang, data any) {
	tpl, ok := rnd.pages[name]
	if !ok {
		log.Printf("web: page template %q not registered", name)
		http.Error(w, i18n.T(lang, "error.internal"), http.StatusInternalServerError)
		return
	}
	tpl, err := bindLang(tpl, lang)
	if err != nil {
		log.Printf("web: bind page %s: %v", name, err)
		http.Error(w, i18n.T(lang, "error.render_page"), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "layout.html", pageData{Title: title, Lang: lang, Data: data}); err != nil {
		log.Printf("web: render page %s: %v", name, err)
		http.Error(w, i18n.T(lang, "error.render_page"), http.StatusInternalServerError)
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
	tpl, err := bindLang(tpl, lang)
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
