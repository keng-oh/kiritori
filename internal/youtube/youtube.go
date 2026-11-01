// Package youtube parses YouTube URLs and checks videos via oEmbed.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ErrUnavailable = errors.New("youtube: video unavailable or not embeddable")

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ParseVideoID accepts a bare video ID or a youtube.com / youtu.be URL.
func ParseVideoID(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if idPattern.MatchString(s) {
		return s, true
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	var id string
	switch host {
	case "youtu.be":
		id = strings.TrimPrefix(u.Path, "/")
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		switch {
		case u.Path == "/watch":
			id = u.Query().Get("v")
		case strings.HasPrefix(u.Path, "/embed/"), strings.HasPrefix(u.Path, "/shorts/"), strings.HasPrefix(u.Path, "/live/"):
			id = strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 3)[1]
		}
	}
	if idPattern.MatchString(id) {
		return id, true
	}
	return "", false
}

type Client struct {
	HTTP *http.Client
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 5 * time.Second}}
}

// Resolve checks that the video exists and is embeddable, returning its title.
func (c *Client) Resolve(ctx context.Context, id string) (string, error) {
	q := url.Values{
		"url":    {"https://www.youtube.com/watch?v=" + id},
		"format": {"json"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.youtube.com/oembed?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return "", ErrUnavailable
	default:
		return "", fmt.Errorf("youtube: oembed status %d", resp.StatusCode)
	}
	var body struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	return body.Title, nil
}
