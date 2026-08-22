package web

import (
	"log"
	"net/http"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/repo"
)

// defaultLang keeps the historical pt-BR UI for installs that never stored
// a locale preference.
const defaultLang = i18n.PtBR

// lang resolves the UI locale for a request. The value is read from
// settings on every request: the single-row lookup is trivially cheap and
// a saved preference applies on the very next navigation without any cache
// invalidation. An unset or unreadable value keeps the pt-BR default; a
// stored-but-unknown value degrades to English.
func (h *Handlers) lang(r *http.Request) i18n.Lang {
	v, err := h.repos.Settings.Get(r.Context(), repo.SettingLocale)
	if err != nil {
		return defaultLang
	}
	if l, ok := i18n.Parse(v); ok {
		return l
	}
	if v == "" {
		return defaultLang
	}
	log.Printf("web: unknown stored locale %q; falling back to en", v)
	return i18n.En
}

func failBadRequest(w http.ResponseWriter, lang i18n.Lang) {
	http.Error(w, i18n.T(lang, "error.form_invalid"), http.StatusBadRequest)
}

func failInternal(w http.ResponseWriter, lang i18n.Lang) {
	http.Error(w, i18n.T(lang, "error.internal"), http.StatusInternalServerError)
}

func failNotFound(w http.ResponseWriter, lang i18n.Lang) {
	http.Error(w, i18n.T(lang, "error.not_found"), http.StatusNotFound)
}
