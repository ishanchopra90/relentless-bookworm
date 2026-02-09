package ol

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// DefaultUserAgent is sent with all Open Library requests so the site can identify
// the crawler and apply robots.txt rules or rate limits.
const DefaultUserAgent = "RelentlessBookworm/1.0 (+https://github.com/relentless-bookworm)"

// RobotsRules holds disallow rules for a given user-agent (e.g. *).
// Path matching follows common practice: Disallow: /search forbids any path whose
// path component starts with /search (e.g. /search, /search.json, /search/authors).
type RobotsRules struct {
	disallowPrefixes []string
}

// Allowed returns false if the URL path is disallowed by the parsed robots.txt
// rules. Path should be the path component of the URL (e.g. /works/OL1.json).
// Empty path or uninitialized rules are treated as allowed. Matching is prefix-based:
// Disallow: /search forbids /search, /search.json, /search/authors, etc.
func (r *RobotsRules) Allowed(path string) bool {
	if r == nil || len(r.disallowPrefixes) == 0 {
		return true
	}
	path = normalizePath(path)
	for _, prefix := range r.disallowPrefixes {
		if strings.HasPrefix(path, prefix) {
			return false
		}
	}
	return true
}

func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		return "/" + p
	}
	return p
}

// FetchRobots fetches robots.txt for the given base URL (e.g. https://openlibrary.org)
// using the provided client. The client should use the same User-Agent we use for
// crawl requests so site-specific rules apply. By convention, fetching /robots.txt
// is always allowed.
func FetchRobots(ctx context.Context, client *http.Client, baseURL string) ([]byte, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/robots.txt"
	u.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &robotsFetchError{status: resp.StatusCode, url: u.String()}
	}
	return io.ReadAll(resp.Body)
}

type robotsFetchError struct {
	status int
	url    string
}

func (e *robotsFetchError) Error() string {
	return "robots.txt fetch failed"
}

// ParseRobots parses robots.txt body and returns rules for the given userAgent.
// Uses the first User-agent block that matches (exact or "*"). Disallow lines
// are collected; path matching is prefix-based.
func ParseRobots(body []byte, userAgent string) *RobotsRules {
	r := &RobotsRules{}
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	var inMatchingBlock bool
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "user-agent:") {
			agent := strings.TrimSpace(line[len("user-agent:"):])
			match := agent == "*" || strings.EqualFold(agent, userAgent)
			if match && !inMatchingBlock {
				inMatchingBlock = true
			} else {
				inMatchingBlock = false
			}
			continue
		}
		if inMatchingBlock && strings.HasPrefix(strings.ToLower(line), "disallow:") {
			path := strings.TrimSpace(line[len("disallow:"):])
			if path != "" {
				path = normalizePath(path)
				r.disallowPrefixes = append(r.disallowPrefixes, path)
			}
		}
	}
	return r
}

// PathFromURL returns the path component of rawURL, or "" if parsing fails.
func PathFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return normalizePath(u.Path)
}
