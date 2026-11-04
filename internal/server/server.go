package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kiritori/internal/store"
	"kiritori/internal/youtube"
	"kiritori/web"
)

const (
	maxClipSeconds = 60
	maxTitleRunes  = 100
	maxFormBytes   = 8 << 10

	sessionCookie  = "kiritori_session"
	sessionTTL     = 30 * 24 * time.Hour
	oauthCookie    = "kiritori_oauth"
	oauthCookieTTL = 10 * time.Minute
)

type ClipStore interface {
	CreateClip(ctx context.Context, in store.NewClip) (store.Clip, error)
	GetClip(ctx context.Context, id string) (store.Clip, error)
}

// VideoResolver checks a video exists and is embeddable, returning its title.
type VideoResolver interface {
	Resolve(ctx context.Context, id string) (string, error)
}

type SessionStore interface {
	UpsertUserByGoogleSub(ctx context.Context, sub string) (store.User, error)
	CreateSession(ctx context.Context, tokenHash []byte, userID string, expiresAt time.Time) error
	GetSessionUser(ctx context.Context, tokenHash []byte) (store.User, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
}

// Authenticator runs the Google login. Exchange returns the user's subject ID.
type Authenticator interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier, nonce string) (sub string, err error)
}

type Deps struct {
	Store    ClipStore
	Resolver VideoResolver
	// Auth と Sessions の両方がそろったときだけログイン機能が有効になる
	Auth     Authenticator
	Sessions SessionStore
}

type server struct {
	Deps
	pages map[string]*template.Template
}

func New(d Deps) (http.Handler, error) {
	s := &server{Deps: d, pages: map[string]*template.Template{}}
	for _, name := range []string{"home", "new", "clip"} {
		t, err := template.ParseFS(web.FS, "templates/base.html", "templates/"+name+".html")
		if err != nil {
			return nil, err
		}
		s.pages[name] = t
	}
	static, err := fs.Sub(web.FS, "static")
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /new", s.newForm)
	mux.HandleFunc("POST /clips", s.createClip)
	mux.HandleFunc("GET /c/{id}", s.showClip)
	if s.loginEnabled() {
		mux.HandleFunc("GET /login", s.login)
		mux.HandleFunc("GET /auth/callback", s.authCallback)
		mux.HandleFunc("POST /logout", s.logout)
		return s.withUser(mux), nil
	}
	return mux, nil
}

func (s *server) loginEnabled() bool { return s.Auth != nil && s.Sessions != nil }

type ctxKey struct{}

func userFrom(r *http.Request) *store.User {
	u, _ := r.Context().Value(ctxKey{}).(*store.User)
	return u
}

// withUser puts the logged-in user, if any, into the request context.
// Unknown or expired sessions are treated as logged out.
func (s *server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			u, err := s.Sessions.GetSessionUser(r.Context(), hashToken(c.Value))
			switch {
			case err == nil:
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u))
			case !errors.Is(err, store.ErrNotFound):
				log.Printf("get session: %v", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// layout is what every template receives; Page is the handler-specific data.
type layout struct {
	User         *store.User
	LoginEnabled bool
	Page         any
}

func (s *server) render(w http.ResponseWriter, r *http.Request, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	l := layout{User: userFrom(r), LoginEnabled: s.loginEnabled(), Page: data}
	if err := s.pages[page].ExecuteTemplate(w, "base", l); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

func setCookie(w http.ResponseWriter, name, value, path string, maxAge time.Duration) {
	c := &http.Cookie{
		Name: name, Value: value, Path: path,
		MaxAge: int(maxAge / time.Second), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	}
	if maxAge > 0 {
		c.Expires = time.Now().Add(maxAge)
	}
	http.SetCookie(w, c)
}

func clearCookie(w http.ResponseWriter, name, path string) {
	setCookie(w, name, "", path, -time.Second)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var tokens [3]string
	for i := range tokens {
		t, err := randomToken()
		if err != nil {
			log.Printf("login: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		tokens[i] = t
	}
	state, nonce, verifier := tokens[0], tokens[1], tokens[2]
	setCookie(w, oauthCookie, strings.Join(tokens[:], "."), "/auth/", oauthCookieTTL)
	http.Redirect(w, r, s.Auth.AuthCodeURL(state, nonce, verifier), http.StatusFound)
}

func (s *server) authCallback(w http.ResponseWriter, r *http.Request) {
	c, cookieErr := r.Cookie(oauthCookie)
	clearCookie(w, oauthCookie, "/auth/") // 一度きり。成否にかかわらず消す
	bad := func(why string) {
		log.Printf("auth callback rejected: %s", why)
		http.Error(w, "bad request", http.StatusBadRequest)
	}
	q := r.URL.Query()
	if q.Get("error") != "" { // ユーザーが Google 側で拒否した
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if cookieErr != nil {
		bad("no oauth cookie")
		return
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 {
		bad("malformed oauth cookie")
		return
	}
	state, nonce, verifier := parts[0], parts[1], parts[2]
	if state == "" || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
		bad("state mismatch")
		return
	}
	code := q.Get("code")
	if code == "" {
		bad("no code")
		return
	}
	sub, err := s.Auth.Exchange(r.Context(), code, verifier, nonce)
	if err != nil {
		bad("exchange: " + err.Error())
		return
	}

	user, err := s.Sessions.UpsertUserByGoogleSub(r.Context(), sub)
	if err != nil {
		log.Printf("upsert user: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	token, err := randomToken()
	if err != nil {
		log.Printf("session token: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.Sessions.CreateSession(r.Context(), hashToken(token), user.ID, time.Now().Add(sessionTTL)); err != nil {
		log.Printf("create session: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	setCookie(w, sessionCookie, token, "/", sessionTTL)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := s.Sessions.DeleteSession(r.Context(), hashToken(c.Value)); err != nil {
			log.Printf("delete session: %v", err)
		}
	}
	clearCookie(w, sessionCookie, "/")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "home", nil)
}

type clipForm struct {
	URL, Start, End, Title string
}

type newPage struct {
	Form  clipForm
	Error string
}

func (s *server) newForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "new", newPage{})
}

func (s *server) createClip(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	form := clipForm{
		URL:   strings.TrimSpace(r.PostFormValue("url")),
		Start: strings.TrimSpace(r.PostFormValue("start")),
		End:   strings.TrimSpace(r.PostFormValue("end")),
		Title: strings.TrimSpace(r.PostFormValue("title")),
	}
	fail := func(status int, msg string) {
		s.render(w, r, status, "new", newPage{Form: form, Error: msg})
	}

	videoID, ok := youtube.ParseVideoID(form.URL)
	if !ok {
		fail(http.StatusBadRequest, "YouTubeのURLを正しく入力してください。")
		return
	}
	start, err1 := strconv.Atoi(form.Start)
	end, err2 := strconv.Atoi(form.End)
	if err1 != nil || err2 != nil || start < 0 {
		fail(http.StatusBadRequest, "開始・終了は0以上の整数（秒）で入力してください。")
		return
	}
	if n := end - start; n < 1 || n > maxClipSeconds {
		fail(http.StatusBadRequest, "クリップの長さは1〜60秒にしてください。")
		return
	}
	if n := utf8.RuneCountInString(form.Title); n < 1 || n > maxTitleRunes {
		fail(http.StatusBadRequest, "タイトルは1〜100文字で入力してください。")
		return
	}

	srcTitle, err := s.Resolver.Resolve(r.Context(), videoID)
	switch {
	case errors.Is(err, youtube.ErrUnavailable):
		fail(http.StatusUnprocessableEntity, "この動画は存在しないか、埋め込みが許可されていません。")
		return
	case err != nil:
		// 実在確認に失敗しても作成自体は止めない（タイトルなしで保存）
		log.Printf("resolve %s: %v", videoID, err)
		srcTitle = ""
	}
	var srcTitlePtr *string
	if srcTitle != "" {
		srcTitlePtr = &srcTitle
	}

	var userID *string
	if u := userFrom(r); u != nil {
		userID = &u.ID
	}
	clip, err := s.Store.CreateClip(r.Context(), store.NewClip{
		SourceID: videoID, Title: form.Title, SourceTitle: srcTitlePtr,
		StartSec: start, EndSec: end, UserID: userID,
	})
	if err != nil {
		log.Printf("create clip: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/c/"+clip.ID, http.StatusSeeOther)
}

type clipPage struct {
	Clip   store.Clip
	Length int
}

func (s *server) showClip(w http.ResponseWriter, r *http.Request) {
	clip, err := s.Store.GetClip(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		log.Printf("get clip: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, r, http.StatusOK, "clip", clipPage{Clip: clip, Length: clip.EndSec - clip.StartSec})
}
