package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"kiritori/internal/store"
	"kiritori/internal/youtube"
)

type fakeStore struct {
	created []store.NewClip
}

func (f *fakeStore) CreateClip(_ context.Context, in store.NewClip) (store.Clip, error) {
	f.created = append(f.created, in)
	return store.Clip{ID: "abc1234567", SourceID: in.SourceID, Title: in.Title,
		StartSec: in.StartSec, EndSec: in.EndSec, SourceTitle: in.SourceTitle}, nil
}

func (f *fakeStore) GetClip(_ context.Context, id string) (store.Clip, error) {
	if id != "abc1234567" {
		return store.Clip{}, store.ErrNotFound
	}
	return store.Clip{ID: id, SourceID: "dQw4w9WgXcQ", Title: "ほげ", StartSec: 10, EndSec: 40}, nil
}

type fakeResolver struct{ err error }

func (f fakeResolver) Resolve(context.Context, string) (string, error) { return "元動画", f.err }

func newTestServer(t *testing.T, res VideoResolver) (http.Handler, *fakeStore) {
	t.Helper()
	fs := &fakeStore{}
	h, err := New(Deps{Store: fs, Resolver: res})
	if err != nil {
		t.Fatal(err)
	}
	return h, fs
}

func post(h http.Handler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/clips", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestPages(t *testing.T) {
	h, _ := newTestServer(t, fakeResolver{})
	for path, want := range map[string]int{
		"/healthz": 200, "/": 200, "/new": 200, "/c/abc1234567": 200,
		"/c/missing": 404, "/static/watch.js": 200, "/nope": 404,
	} {
		if got := get(h, path).Code; got != want {
			t.Errorf("GET %s = %d, want %d", path, got, want)
		}
	}
}

func TestClipPageContent(t *testing.T) {
	h, _ := newTestServer(t, fakeResolver{})
	body := get(h, "/c/abc1234567").Body.String()
	for _, want := range []string{`data-video-id="dQw4w9WgXcQ"`, `data-start="10"`, `data-end="40"`,
		"watch?v=dQw4w9WgXcQ&amp;t=40s", `property="og:title"`} {
		if !strings.Contains(body, want) {
			t.Errorf("clip page missing %q", want)
		}
	}
}

func TestCreateClip(t *testing.T) {
	valid := url.Values{"url": {"https://youtu.be/dQw4w9WgXcQ"}, "start": {"10"}, "end": {"40"}, "title": {"ほげ"}}
	with := func(k, v string) url.Values {
		c := url.Values{}
		for kk, vv := range valid {
			c[kk] = vv
		}
		c.Set(k, v)
		return c
	}

	tests := []struct {
		name   string
		form   url.Values
		res    error
		status int
		stored int
	}{
		{"ok", valid, nil, http.StatusSeeOther, 1},
		{"bad url", with("url", "https://example.com"), nil, 400, 0},
		{"non numeric", with("start", "abc"), nil, 400, 0},
		{"negative start", with("start", "-1"), nil, 400, 0},
		{"end before start", with("end", "5"), nil, 400, 0},
		{"too long", with("end", "71"), nil, 400, 0},
		{"exactly 60s", with("end", "70"), nil, http.StatusSeeOther, 1},
		{"empty title", with("title", "  "), nil, 400, 0},
		{"long title", with("title", strings.Repeat("あ", 101)), nil, 400, 0},
		{"unavailable video", valid, youtube.ErrUnavailable, 422, 0},
		{"resolver outage still saves", valid, context.DeadlineExceeded, http.StatusSeeOther, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, fs := newTestServer(t, fakeResolver{err: tt.res})
			rec := post(h, tt.form)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if len(fs.created) != tt.stored {
				t.Fatalf("stored = %d, want %d", len(fs.created), tt.stored)
			}
			if tt.status == http.StatusSeeOther && rec.Header().Get("Location") != "/c/abc1234567" {
				t.Fatalf("Location = %q", rec.Header().Get("Location"))
			}
		})
	}
}

type fakeAuth struct {
	sub                  string
	err                  error
	gotCode, gotVerifier string
	gotNonce             string
	exchanged            int
}

func (f *fakeAuth) AuthCodeURL(state, nonce, verifier string) string {
	return "https://accounts.example/auth?state=" + state + "&nonce=" + nonce
}

func (f *fakeAuth) Exchange(_ context.Context, code, verifier, nonce string) (string, error) {
	f.exchanged++
	f.gotCode, f.gotVerifier, f.gotNonce = code, verifier, nonce
	return f.sub, f.err
}

type fakeSessions struct {
	users   map[string]store.User // key: string(tokenHash)
	created map[string]time.Time
	deleted [][]byte
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{users: map[string]store.User{}, created: map[string]time.Time{}}
}

func (f *fakeSessions) UpsertUserByGoogleSub(_ context.Context, sub string) (store.User, error) {
	return store.User{ID: "user-" + sub, DisplayName: "ユーザーabc123"}, nil
}

func (f *fakeSessions) CreateSession(_ context.Context, hash []byte, userID string, exp time.Time) error {
	f.users[string(hash)] = store.User{ID: userID, DisplayName: "ユーザーabc123"}
	f.created[string(hash)] = exp
	return nil
}

func (f *fakeSessions) GetSessionUser(_ context.Context, hash []byte) (store.User, error) {
	u, ok := f.users[string(hash)]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeSessions) DeleteSession(_ context.Context, hash []byte) error {
	f.deleted = append(f.deleted, hash)
	delete(f.users, string(hash))
	return nil
}

func newAuthServer(t *testing.T) (http.Handler, *fakeStore, *fakeAuth, *fakeSessions) {
	t.Helper()
	fs, fa, ss := &fakeStore{}, &fakeAuth{sub: "g-sub"}, newFakeSessions()
	h, err := New(Deps{Store: fs, Resolver: fakeResolver{}, Auth: fa, Sessions: ss})
	if err != nil {
		t.Fatal(err)
	}
	return h, fs, fa, ss
}

func cookieNamed(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func request(h http.Handler, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// startLogin performs GET /login and returns the oauth cookie and the state sent to Google.
func startLogin(t *testing.T, h http.Handler) (*http.Cookie, string) {
	t.Helper()
	rec := request(h, "GET", "/login")
	if rec.Code != http.StatusFound {
		t.Fatalf("/login = %d, want 302", rec.Code)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || loc.Host != "accounts.example" {
		t.Fatalf("Location = %q", rec.Header().Get("Location"))
	}
	c := cookieNamed(rec, "kiritori_oauth")
	if c == nil {
		t.Fatal("no kiritori_oauth cookie")
	}
	return c, loc.Query().Get("state")
}

// login completes a login and returns the session cookie.
func login(t *testing.T, h http.Handler) *http.Cookie {
	t.Helper()
	oc, state := startLogin(t, h)
	rec := request(h, "GET", "/auth/callback?code=c&state="+url.QueryEscape(state), oc)
	c := cookieNamed(rec, "kiritori_session")
	if rec.Code != http.StatusSeeOther || c == nil {
		t.Fatalf("callback = %d, session cookie = %v", rec.Code, c)
	}
	return &http.Cookie{Name: c.Name, Value: c.Value}
}

func TestLoginRedirect(t *testing.T) {
	h, _, _, _ := newAuthServer(t)
	oc, state := startLogin(t, h)
	if !oc.HttpOnly || !oc.Secure || oc.SameSite != http.SameSiteLaxMode || oc.Path != "/auth/" || oc.MaxAge != 600 {
		t.Fatalf("oauth cookie attributes: %+v", oc)
	}
	if state == "" || !strings.HasPrefix(oc.Value, state+".") {
		t.Fatalf("state %q is not carried in cookie %q", state, oc.Value)
	}
}

func TestAuthCallback(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		h, _, fa, ss := newAuthServer(t)
		oc, state := startLogin(t, h)
		parts := strings.Split(oc.Value, ".")

		rec := request(h, "GET", "/auth/callback?code=the-code&state="+url.QueryEscape(state), oc)
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
			t.Fatalf("callback = %d %q", rec.Code, rec.Header().Get("Location"))
		}
		if fa.gotCode != "the-code" || fa.gotNonce != parts[1] || fa.gotVerifier != parts[2] {
			t.Fatalf("Exchange args = %q %q %q", fa.gotCode, fa.gotNonce, fa.gotVerifier)
		}
		if gone := cookieNamed(rec, "kiritori_oauth"); gone == nil || gone.MaxAge >= 0 {
			t.Fatalf("oauth cookie not cleared: %+v", gone)
		}

		sc := cookieNamed(rec, "kiritori_session")
		if sc == nil {
			t.Fatal("no session cookie")
		}
		if !sc.HttpOnly || !sc.Secure || sc.SameSite != http.SameSiteLaxMode || sc.Path != "/" ||
			sc.MaxAge != 30*24*60*60 {
			t.Fatalf("session cookie attributes: %+v", sc)
		}
		hash := sha256.Sum256([]byte(sc.Value))
		exp, ok := ss.created[string(hash[:])]
		if !ok || len(ss.created) != 1 {
			t.Fatalf("session must be stored under sha256(token); created = %d", len(ss.created))
		}
		if d := time.Until(exp) - 30*24*time.Hour; d > time.Minute || d < -time.Minute {
			t.Fatalf("expiry = %v, want about 30 days from now", exp)
		}
		if u := ss.users[string(hash[:])]; u.ID != "user-g-sub" {
			t.Fatalf("session user = %+v", u)
		}
	})

	tests := []struct {
		name  string
		query func(state string) string
		keep  bool // send the oauth cookie
		err   error
		want  int
	}{
		{"state mismatch", func(string) string { return "?code=c&state=forged" }, true, nil, 400},
		{"missing state", func(string) string { return "?code=c" }, true, nil, 400},
		{"no oauth cookie", func(s string) string { return "?code=c&state=" + s }, false, nil, 400},
		{"missing code", func(s string) string { return "?state=" + s }, true, nil, 400},
		{"exchange fails (e.g. nonce mismatch)", func(s string) string { return "?code=c&state=" + s }, true, errors.New("nonce mismatch"), 400},
		{"denied at Google", func(string) string { return "?error=access_denied" }, true, nil, 303},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, fa, ss := newAuthServer(t)
			fa.err = tt.err
			oc, state := startLogin(t, h)
			var cookies []*http.Cookie
			if tt.keep {
				cookies = append(cookies, oc)
			}
			rec := request(h, "GET", "/auth/callback"+tt.query(url.QueryEscape(state)), cookies...)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if cookieNamed(rec, "kiritori_session") != nil || len(ss.created) != 0 {
				t.Fatal("a session must not be created")
			}
		})
	}
}

func TestMalformedOAuthCookie(t *testing.T) {
	h, _, fa, ss := newAuthServer(t)
	rec := request(h, "GET", "/auth/callback?code=c&state=x", &http.Cookie{Name: "kiritori_oauth", Value: "x"})
	if rec.Code != 400 || fa.exchanged != 0 || len(ss.created) != 0 {
		t.Fatalf("status = %d, exchanged = %d, sessions = %d", rec.Code, fa.exchanged, len(ss.created))
	}
}

func TestCreateClipRecordsUser(t *testing.T) {
	form := url.Values{"url": {"https://youtu.be/dQw4w9WgXcQ"}, "start": {"10"}, "end": {"40"}, "title": {"ほげ"}}
	send := func(h http.Handler, c *http.Cookie) {
		req := httptest.NewRequest("POST", "/clips", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if c != nil {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d", rec.Code)
		}
	}

	h, fs, _, _ := newAuthServer(t)
	send(h, nil)
	send(h, login(t, h))
	if len(fs.created) != 2 {
		t.Fatalf("created = %d", len(fs.created))
	}
	if fs.created[0].UserID != nil {
		t.Errorf("anonymous clip has UserID %q", *fs.created[0].UserID)
	}
	if got := fs.created[1].UserID; got == nil || *got != "user-g-sub" {
		t.Errorf("logged-in clip UserID = %v", got)
	}
}

func TestLoginNotice(t *testing.T) {
	const notice = "ログインすると一覧・ランキングに載り、編集・削除もできる"
	h, _, _, _ := newAuthServer(t)

	if body := request(h, "GET", "/new").Body.String(); !strings.Contains(body, notice) || !strings.Contains(body, `href="/login"`) {
		t.Error("anonymous /new should show the login notice and link")
	}
	sc := login(t, h)
	body := request(h, "GET", "/new", sc).Body.String()
	if strings.Contains(body, notice) || strings.Contains(body, `href="/login"`) || !strings.Contains(body, `action="/logout"`) {
		t.Error("logged-in /new should show logout, not the notice")
	}

	// セッションが DB に無い（期限切れ）Cookie は未ログイン扱い
	stale := &http.Cookie{Name: "kiritori_session", Value: "expired-token"}
	if body := request(h, "GET", "/new", stale).Body.String(); !strings.Contains(body, notice) {
		t.Error("expired session should be treated as logged out")
	}
}

func TestLogout(t *testing.T) {
	h, _, _, ss := newAuthServer(t)
	sc := login(t, h)
	hash := sha256.Sum256([]byte(sc.Value))

	rec := request(h, "POST", "/logout", sc)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("logout = %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(ss.deleted) != 1 || string(ss.deleted[0]) != string(hash[:]) {
		t.Fatalf("DeleteSession calls = %v", ss.deleted)
	}
	if c := cookieNamed(rec, "kiritori_session"); c == nil || c.MaxAge >= 0 {
		t.Fatalf("session cookie not cleared: %+v", c)
	}
	if body := request(h, "GET", "/new", sc).Body.String(); !strings.Contains(body, `href="/login"`) {
		t.Error("session must be unusable after logout")
	}
}

func TestLoginDisabled(t *testing.T) {
	h, _ := newTestServer(t, fakeResolver{})
	for _, tc := range []struct{ method, path string }{
		{"GET", "/login"}, {"GET", "/auth/callback"}, {"POST", "/logout"},
	} {
		if got := request(h, tc.method, tc.path).Code; got == http.StatusOK || got == http.StatusFound || got == http.StatusSeeOther {
			t.Errorf("%s %s = %d, want it unavailable", tc.method, tc.path, got)
		}
	}
	if got := request(h, "GET", "/login").Code; got != 404 {
		t.Errorf("GET /login = %d, want 404", got)
	}
	body := request(h, "GET", "/new").Body.String()
	for _, bad := range []string{"/login", "/logout", "ログイン"} {
		if strings.Contains(body, bad) {
			t.Errorf("/new mentions %q while login is disabled", bad)
		}
	}
	if body := request(h, "GET", "/").Body.String(); strings.Contains(body, "/login") {
		t.Error("home mentions login while disabled")
	}
}
