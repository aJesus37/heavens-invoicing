package web

import (
	"errors"
	"log"
	"net/http"

	"github.com/jesus/invoice-app/internal/auth"
	"github.com/jesus/invoice-app/internal/i18n"
)

// loginData feeds the login/setup page. Error is pre-localized; CSRFToken
// is the double-submit value planted by the GET handler so the form POST
// can prove it originated from this page.
type loginData struct {
	Setup     bool
	Error     string
	CSRFToken string
}

// loginForm serves GET /login: the first-run "set admin password" form
// while no password is configured, the sign-in form afterwards.
func (h *Handlers) loginForm(w http.ResponseWriter, r *http.Request) {
	lang := h.lang(r)
	// Plant the CSRF double-submit cookie here so the form POST can prove
	// it came from this page rather than a forged cross-site request.
	csrf, err := h.auth.IssueCSRFToken(w)
	if err != nil {
		log.Printf("web: issue csrf on login: %v", err)
		failInternal(w, lang)
		return
	}
	setup, err := h.auth.NeedsSetup(r.Context())
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}
	h.renderLogin(w, lang, loginData{Setup: setup, CSRFToken: csrf}, http.StatusOK)
}

// loginSubmit handles POST /login for both phases:
//
//   - setup (no stored hash): validates and stores the admin password,
//     then logs straight in so first run ends on the dashboard;
//   - normal: verifies against the stored bcrypt hash.
//
// The redirect target after success is always "/" — a ?next= parameter is
// deliberately not honored, since it is a classic open-redirect vector on
// login forms.
func (h *Handlers) loginSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := h.lang(r)
	if err := r.ParseForm(); err != nil {
		failBadRequest(w, lang)
		return
	}

	ip := auth.ClientIP(r.RemoteAddr)
	if !h.auth.AllowLogin(ip) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, i18n.T(lang, "auth.error_locked"), http.StatusTooManyRequests)
		return
	}

	setup, err := h.auth.NeedsSetup(ctx)
	if err != nil {
		writeRepoErr(w, lang, err)
		return
	}

	password := r.PostFormValue("password")

	if setup {
		confirm := r.PostFormValue("confirm")
		switch {
		case len(password) < auth.MinPasswordLength:
			h.renderLogin(w, lang, loginData{Setup: true,
				Error: i18n.T(lang, "auth.error_short")}, http.StatusBadRequest)
			return
		case password != confirm:
			h.renderLogin(w, lang, loginData{Setup: true,
				Error: i18n.T(lang, "auth.error_mismatch")}, http.StatusBadRequest)
			return
		}
		if err := h.auth.SetPassword(ctx, password); err != nil {
			if errors.Is(err, auth.ErrWeakPassword) {
				h.renderLogin(w, lang, loginData{Setup: true,
					Error: i18n.T(lang, "auth.error_short")}, http.StatusBadRequest)
				return
			}
			writeRepoErr(w, lang, err)
			return
		}
	} else {
		ok, err := h.auth.VerifyPassword(ctx, password)
		if err != nil {
			writeRepoErr(w, lang, err)
			return
		}
		if !ok {
			h.auth.LoginFailure(ip)
			h.renderLogin(w, lang, loginData{
				Error: i18n.T(lang, "auth.error_invalid")}, http.StatusUnauthorized)
			return
		}
	}

	if err := h.auth.StartSession(ctx, w); err != nil {
		log.Printf("web: start session: %v", err)
		failInternal(w, lang)
		return
	}
	h.auth.LoginSuccess(ip)

	// Lazy cleanup choice: prune expired sessions here instead of running
	// a background sweeper goroutine — one indexed DELETE per successful
	// login keeps the table small with nothing to supervise.
	if err := h.auth.SweepExpired(ctx); err != nil {
		log.Printf("web: expired-session sweep: %v", err)
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logout handles POST /logout: destroys the server-side session and clears
// the auth cookies. Session + CSRF validity were already enforced by the
// auth gate before this handler runs.
func (h *Handlers) logout(w http.ResponseWriter, r *http.Request) {
	h.auth.EndSession(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// renderLogin renders the login page at an explicit status so failures
// (wrong password, invalid form) surface real status codes.
func (h *Handlers) renderLogin(w http.ResponseWriter, lang i18n.Lang, data loginData, status int) {
	titleKey := "auth.login_title"
	if data.Setup {
		titleKey = "auth.setup_title"
	}
	// Login visitors are by definition unauthenticated; render through
	// the renderer directly with empty chrome (no nav). The CSRF token is
	// threaded through so the form embeds it for the POST.
	h.render.renderPage(w, status, "login.html", pageData{
		Title:     i18n.T(lang, titleKey),
		Lang:      lang,
		CSRFToken: data.CSRFToken,
		Data:      data,
	})
}
