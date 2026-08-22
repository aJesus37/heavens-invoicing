package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/auth"
	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/jesus/invoice-app/internal/server"
	"github.com/jesus/invoice-app/internal/web"
)

// newAuthEnv boots the full stack WITH the auth gate, mirroring production
// wiring, so login/CSRF/session behavior is exercised end to end.
func newAuthEnv(t *testing.T) (*httptest.Server, *repo.Repos, *auth.Manager) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	repos := repo.New(conn)
	am := auth.New(repos.Sessions, repos.Settings)
	router := deliver.NewRouter(repos.Invoices, nil, nil, nil, nil, nil)
	handlers, err := web.New(repos, router, nil, pdf.SenderInfo{}, am)
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	srv := server.New(server.Config{Web: handlers.Mux(), Auth: am})
	ts := httptest.NewServer(srv.Handler())
	ts.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return ts, repos, am
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func doForm(t *testing.T, ts *httptest.Server, method, path string, form url.Values, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, ts.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func getReq(t *testing.T, ts *httptest.Server, path string, cookies ...*http.Cookie) *http.Response {
	return doForm(t, ts, http.MethodGet, path, nil, cookies...)
}

func readBody(resp *http.Response) string {
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return string(b)
}

// loginOnce drives first-run setup and returns the resulting session
// cookie so tests can act as the logged-in admin.
func loginOnce(t *testing.T, ts *httptest.Server) *http.Cookie {
	lr := getReq(t, ts, "/login")
	csrf := cookieNamed(lr, "csrf_token")
	lr.Body.Close()
	if csrf == nil {
		t.Fatal("no CSRF cookie on /login")
	}
	set := doForm(t, ts, http.MethodPost, "/login", url.Values{
		"password":   {"supersecret1"},
		"confirm":    {"supersecret1"},
		"csrf_token": {csrf.Value},
	}, csrf)
	sess := cookieNamed(set, "session")
	set.Body.Close()
	if sess == nil {
		t.Fatal("setup did not establish a session")
	}
	return sess
}

func TestFirstRunSetupFlow(t *testing.T) {
	ts, _, _ := newAuthEnv(t)

	// First GET /login: no password configured → setup form with a
	// confirm field and a freshly issued CSRF cookie.
	resp := getReq(t, ts, "/login")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup GET: got %d want 200", resp.StatusCode)
	}
	body := readBody(resp)
	if !strings.Contains(body, `name="confirm"`) {
		t.Error("setup form must ask to confirm the password")
	}
	if !strings.Contains(body, `name="csrf_token"`) {
		t.Error("setup form must carry a CSRF field")
	}
	csrf := cookieNamed(resp, "csrf_token")
	if csrf == nil || csrf.Value == "" {
		t.Fatal("GET /login must set a CSRF cookie")
	}

	// Empty password is refused (400) before any lockout logic.
	empty := doForm(t, ts, http.MethodPost, "/login", url.Values{
		"password":   {""},
		"confirm":    {""},
		"csrf_token": {csrf.Value},
	}, csrf)
	if empty.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty password: got %d want 400", empty.StatusCode)
	}
	empty.Body.Close()

	// Correct setup POST logs straight in (303 → /) and sets a session.
	postOK := doForm(t, ts, http.MethodPost, "/login", url.Values{
		"password":   {"supersecret1"},
		"confirm":    {"supersecret1"},
		"csrf_token": {csrf.Value},
	}, csrf)
	if postOK.StatusCode != http.StatusSeeOther || postOK.Header.Get("Location") != "/" {
		t.Fatalf("setup submit: got %d loc=%q want 303 /", postOK.StatusCode, postOK.Header.Get("Location"))
	}
	sess := cookieNamed(postOK, "session")
	postOK.Body.Close()
	if sess == nil {
		t.Fatal("successful setup must set a session cookie")
	}

	// The new session reaches the dashboard.
	dash := getReq(t, ts, "/", sess)
	if dash.StatusCode != http.StatusOK {
		t.Fatalf("authed dashboard: got %d want 200", dash.StatusCode)
	}
	dash.Body.Close()

	// Second visit to /login now shows the normal sign-in (no confirm).
	login2 := getReq(t, ts, "/login", sess)
	if login2.StatusCode != http.StatusOK {
		t.Fatalf("login GET when set: got %d want 200", login2.StatusCode)
	}
	body2 := readBody(login2)
	if strings.Contains(body2, `name="confirm"`) {
		t.Error("once configured, /login must not ask to confirm a password")
	}
}

func TestLoginWrongPasswordLockoutThenRecover(t *testing.T) {
	ts, _, am := newAuthEnv(t)

	// Seed a password and a lockable clock.
	now := time.Unix(1_700_000_000, 0)
	am.Now = func() time.Time { return now }
	if err := am.SetPassword(context.Background(), "correct-horse1"); err != nil {
		t.Fatal(err)
	}

	lr := getReq(t, ts, "/login")
	csrf := cookieNamed(lr, "csrf_token")
	lr.Body.Close()
	if csrf == nil {
		t.Fatal("no CSRF cookie on /login")
	}

	// Five wrong attempts are each rejected with 401; the limiter records
	// them and locks the address only after the fifth.
	for i := 0; i < auth.MaxLoginFailures; i++ {
		r := doForm(t, ts, http.MethodPost, "/login", url.Values{
			"password":   {"wrong"},
			"csrf_token": {csrf.Value},
		}, csrf)
		if r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong attempt %d: got %d want 401", i+1, r.StatusCode)
		}
		r.Body.Close()
	}

	// Now locked: both a wrong and the correct password are rejected 429.
	for _, pw := range []string{"wrong", "correct-horse1"} {
		r := doForm(t, ts, http.MethodPost, "/login", url.Values{
			"password":   {pw},
			"csrf_token": {csrf.Value},
		}, csrf)
		if r.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("locked attempt (pw=%q): got %d want 429", pw, r.StatusCode)
		}
		r.Body.Close()
	}

	// Advance the clock past the lockout window; the counter resets so the
	// correct password logs in again.
	now = now.Add(2 * time.Minute)
	ok := doForm(t, ts, http.MethodPost, "/login", url.Values{
		"password":   {"correct-horse1"},
		"csrf_token": {csrf.Value},
	}, csrf)
	if ok.StatusCode != http.StatusSeeOther {
		t.Fatalf("post-lockout login: got %d want 303", ok.StatusCode)
	}
	ok.Body.Close()
}

func TestLogoutInvalidatesSession(t *testing.T) {
	ts, _, _ := newAuthEnv(t)
	sess := loginOnce(t, ts)

	// Authenticated dashboard works.
	dash := getReq(t, ts, "/", sess)
	if dash.StatusCode != http.StatusOK {
		t.Fatalf("pre-logout dashboard: got %d want 200", dash.StatusCode)
	}
	dash.Body.Close()

	// Fetch a CSRF token to authorize the logout POST.
	lr := getReq(t, ts, "/login", sess)
	csrf := cookieNamed(lr, "csrf_token")
	lr.Body.Close()
	if csrf == nil {
		t.Fatal("no CSRF cookie available for logout")
	}

	// Logout (with CSRF) clears the session server-side and redirects.
	out := doForm(t, ts, http.MethodPost, "/logout", url.Values{
		"csrf_token": {csrf.Value},
	}, sess, csrf)
	if out.StatusCode != http.StatusSeeOther || out.Header.Get("Location") != "/login" {
		t.Fatalf("logout: got %d loc=%q want 303 /login", out.StatusCode, out.Header.Get("Location"))
	}
	out.Body.Close()

	// The old session cookie no longer grants access.
	again := getReq(t, ts, "/", sess)
	if again.StatusCode != http.StatusSeeOther || again.Header.Get("Location") != "/login" {
		t.Fatalf("post-logout reused cookie: got %d loc=%q want 303 /login", again.StatusCode, again.Header.Get("Location"))
	}
	again.Body.Close()
}
