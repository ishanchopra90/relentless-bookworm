package ol

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// EnsureJSONURL appends .json to Open Library URLs when missing.
func EnsureJSONURL(url string) string {
	if strings.HasSuffix(url, ".json") {
		return url
	}
	return strings.TrimRight(url, "/") + ".json"
}

// SearchURL builds a Search API URL for a query string.
func SearchURL(query string) string {
	return "https://openlibrary.org/search.json?q=" + url.QueryEscape(query)
}

// SearchAuthorURL builds a Search API URL for an author name.
func SearchAuthorURL(author string) string {
	return "https://openlibrary.org/search.json?author=" + url.QueryEscape(author)
}

// SearchSubjectURL builds a Search API URL for a subject.
func SearchSubjectURL(subject string) string {
	return "https://openlibrary.org/search.json?subject=" + url.QueryEscape(subject)
}

// FetchJSON retrieves the raw JSON for an Open Library URL using http.DefaultClient.
func FetchJSON(ctx context.Context, url string) ([]byte, error) {
	return FetchJSONWithClient(ctx, http.DefaultClient, url)
}

// FetchJSONWithClient retrieves the raw JSON for an Open Library URL using the given HTTP client
// (e.g. one configured with a proxy for multi-egress / rate-limit bypass).
// Sets a custom User-Agent (DefaultUserAgent) so the site can identify the crawler.
func FetchJSONWithClient(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", DefaultUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}
