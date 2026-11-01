package youtube

import "testing"

func TestParseVideoID(t *testing.T) {
	const id = "dQw4w9WgXcQ"
	ok := []string{
		id,
		"https://www.youtube.com/watch?v=" + id,
		"https://www.youtube.com/watch?v=" + id + "&t=30s",
		"https://youtu.be/" + id + "?t=5",
		"https://m.youtube.com/watch?v=" + id,
		"https://www.youtube.com/embed/" + id,
		"https://www.youtube.com/shorts/" + id,
		"https://www.youtube.com/live/" + id + "?feature=share",
		" https://youtu.be/" + id + " ",
	}
	for _, s := range ok {
		if got, found := ParseVideoID(s); !found || got != id {
			t.Errorf("ParseVideoID(%q) = %q, %v", s, got, found)
		}
	}
	bad := []string{
		"", "hello", "https://example.com/watch?v=" + id,
		"https://www.youtube.com/watch?v=short", "https://www.youtube.com/",
		"https://www.youtube.com/embed/", "ftp://youtu.be/" + id,
	}
	for _, s := range bad {
		if got, found := ParseVideoID(s); found {
			t.Errorf("ParseVideoID(%q) = %q, want not found", s, got)
		}
	}
}
