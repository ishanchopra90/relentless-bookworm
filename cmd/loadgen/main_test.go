package main

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// mockTransport records the last request and returns a configurable status.
type mockTransport struct {
	mu         sync.Mutex
	status     int
	lastURL    string
	lastMethod string
	reqCount   int
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	m.lastURL = req.URL.String()
	m.lastMethod = req.Method
	m.reqCount++
	m.mu.Unlock()
	return &http.Response{
		StatusCode: m.status,
		Body:       http.NoBody,
		Header:     make(http.Header),
	}, nil
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()

	validPath := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(validPath, []byte(`{"seeds":["https://openlibrary.org/works/OL45883W"]}`), 0644); err != nil {
		t.Fatal(err)
	}

	emptyPath := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(emptyPath, []byte(`{"seeds":[]}`), 0644); err != nil {
		t.Fatal(err)
	}

	badJSONPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badJSONPath, []byte(`{not json`), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
		seeds   int
	}{
		{"valid", validPath, false, 1},
		{"missing", filepath.Join(dir, "missing.json"), true, 0},
		{"empty seeds", emptyPath, true, 0},
		{"invalid json", badJSONPath, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := loadConfig(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadConfig() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(cfg.Seeds) != tt.seeds {
				t.Errorf("len(Seeds) = %d, want %d", len(cfg.Seeds), tt.seeds)
			}
			if tt.name == "empty seeds" && err != errNoSeeds {
				t.Errorf("empty seeds: err = %v, want errNoSeeds", err)
			}
		})
	}
}

func TestSubmitSeed(t *testing.T) {
	transport := &mockTransport{status: http.StatusAccepted}
	client := &http.Client{Transport: transport}
	baseURL, _ := url.Parse("http://api.test")

	submitSeed(client, baseURL, 0, "https://openlibrary.org/works/OL45883W")

	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.lastMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", transport.lastMethod)
	}
	parsed, _ := url.Parse(transport.lastURL)
	wantQuery := url.Values{"url": {"https://openlibrary.org/works/OL45883W"}}.Encode()
	if parsed.Path != "/crawl" || parsed.RawQuery != wantQuery {
		t.Errorf("url = %s (path=%q query=%q), want path=/crawl query=%q", transport.lastURL, parsed.Path, parsed.RawQuery, wantQuery)
	}
}

func TestSubmitSeed_nonAccepted(t *testing.T) {
	transport := &mockTransport{status: http.StatusBadRequest}
	client := &http.Client{Transport: transport}
	baseURL, _ := url.Parse("http://api.test")
	submitSeed(client, baseURL, 0, "bad") // should not panic
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "seeds.json")
	if err := os.WriteFile(configPath, []byte(`{"seeds":["a","b","c"]}`), 0644); err != nil {
		t.Fatal(err)
	}

	transport := &mockTransport{status: http.StatusAccepted}
	client := &http.Client{Transport: transport}

	if err := run(configPath, "http://api.test", client); err != nil {
		t.Fatalf("run() err = %v", err)
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.reqCount != 3 {
		t.Errorf("request count = %d, want 3", transport.reqCount)
	}
}

func TestRun_badConfigPath(t *testing.T) {
	if err := run("/nonexistent/config.json", "http://localhost:8080", nil); err == nil {
		t.Fatal("run() expected error for missing config")
	}
}

func TestRun_emptySeeds(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(configPath, []byte(`{"seeds":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(configPath, "http://localhost:8080", nil); err != errNoSeeds {
		t.Fatalf("run() err = %v, want errNoSeeds", err)
	}
}

func TestRun_invalidAPIBase(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "seeds.json")
	if err := os.WriteFile(configPath, []byte(`{"seeds":["a"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run(configPath, "://invalid", nil); err == nil {
		t.Fatal("run() expected error for invalid api base")
	}
}
