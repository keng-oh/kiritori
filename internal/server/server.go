package server

import (
	"context"
	"errors"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"kiritori/internal/store"
	"kiritori/internal/youtube"
	"kiritori/web"
)

const (
	maxClipSeconds = 60
	maxTitleRunes  = 100
	maxFormBytes   = 8 << 10
)

type ClipStore interface {
	CreateClip(ctx context.Context, in store.NewClip) (store.Clip, error)
	GetClip(ctx context.Context, id string) (store.Clip, error)
}

// VideoResolver checks a video exists and is embeddable, returning its title.
type VideoResolver interface {
	Resolve(ctx context.Context, id string) (string, error)
}

type Deps struct {
	Store    ClipStore
	Resolver VideoResolver
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
	return mux, nil
}

func (s *server) render(w http.ResponseWriter, status int, page string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.pages[page].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("render %s: %v", page, err)
	}
}

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "home", nil)
}

type clipForm struct {
	URL, Start, End, Title string
}

type newPage struct {
	Form  clipForm
	Error string
}

func (s *server) newForm(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, "new", newPage{})
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
		s.render(w, status, "new", newPage{Form: form, Error: msg})
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

	clip, err := s.Store.CreateClip(r.Context(), store.NewClip{
		SourceID: videoID, Title: form.Title, SourceTitle: srcTitlePtr,
		StartSec: start, EndSec: end,
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
	s.render(w, http.StatusOK, "clip", clipPage{Clip: clip, Length: clip.EndSec - clip.StartSec})
}
