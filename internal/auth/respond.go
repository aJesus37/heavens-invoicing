package auth

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/repo"
)

const (
	repoSettingLocale = repo.SettingLocale
	defaultUILang     = i18n.PtBR
)

type apiError struct {
	Error string `json:"error"`
}

// writeJSONError emits the API's error shape.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: msg})
}

// uiLang resolves the stored UI locale for rejection messages, mirroring
// web.lang's read (settings lookup each time; unset/unknown falls back to
// pt-BR via Resolve). Kept local to avoid an import cycle with package
// web; errors degrade silently because this only picks a language.
func (m *Manager) uiLang(ctx context.Context) i18n.Lang {
	v, err := m.settings.Get(ctx, repoSettingLocale)
	if err != nil {
		return defaultUILang
	}
	return i18n.Resolve(v)
}

// rejectInternal answers a fail-closed session-store error: JSON for the
// API, localized plain text for pages.
func (m *Manager) rejectInternal(ctx context.Context, w http.ResponseWriter, path string) {
	if isAPIRequest(path) {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.Error(w, i18n.T(m.uiLang(ctx), "error.internal"), http.StatusInternalServerError)
}

// rejectForbidden answers a failed CSRF check: JSON for the API, localized
// plain text for pages. Both carry 403 so compliant clients (and tests)
// can distinguish a missing session (401/303) from a missing token.
func (m *Manager) rejectForbidden(ctx context.Context, w http.ResponseWriter, path string) {
	if isAPIRequest(path) {
		writeJSONError(w, http.StatusForbidden, "csrf token invalid")
		return
	}
	http.Error(w, i18n.T(m.uiLang(ctx), "auth.error_csrf"), http.StatusForbidden)
}
