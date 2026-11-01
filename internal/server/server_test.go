package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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
