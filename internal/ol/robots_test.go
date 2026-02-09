package ol

import (
	"testing"
)

func TestParseRobots_Allowed(t *testing.T) {
	// Open Library-style: User-agent * with Disallow /search, /api, etc.
	body := `
User-agent: *
Disallow: /api
Disallow: /edit
Disallow: /search

User-agent: Googlebot
Crawl-delay: 10
`
	r := ParseRobots([]byte(body), DefaultUserAgent)

	for _, path := range []string{"/works/OL1W.json", "/authors/OL1A.json", "/books/OL1M.json"} {
		if !r.Allowed(path) {
			t.Errorf("expected path %q to be allowed", path)
		}
	}
	for _, path := range []string{"/search", "/search.json", "/search/authors", "/api/books", "/edit"} {
		if r.Allowed(path) {
			t.Errorf("expected path %q to be disallowed", path)
		}
	}
}

func TestParseRobots_NilEmptyAllowed(t *testing.T) {
	var r *RobotsRules
	if !r.Allowed("/anything") {
		t.Error("nil rules should allow all")
	}
	empty := ParseRobots([]byte("User-agent: *\n"), DefaultUserAgent)
	if !empty.Allowed("/search") {
		t.Error("empty disallow list should allow all")
	}
}

func TestPathFromURL(t *testing.T) {
	if got := PathFromURL("https://openlibrary.org/works/OL1W.json"); got != "/works/OL1W.json" {
		t.Errorf("PathFromURL = %q", got)
	}
	if got := PathFromURL("https://openlibrary.org/search.json?q=foo"); got != "/search.json" {
		t.Errorf("PathFromURL = %q", got)
	}
	if got := PathFromURL(""); got != "/" {
		t.Errorf("PathFromURL empty = %q", got)
	}
}
