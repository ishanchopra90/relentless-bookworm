package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/segmentio/kafka-go"

	"relentless-bookworm/common"
	"relentless-bookworm/internal/models"
	"relentless-bookworm/mocks"
)

// newTestWorker creates a worker with commit channel and wait group for tests.
func newTestWorker(reader messageReader, store dedupeStore, results, edges, frontier, dlq resultWriter, ttl time.Duration, maxDepth, retryMax int, retryBase, retryMaxDelay time.Duration) (*worker, chan kafka.Message, *sync.WaitGroup) {
	commitCh := make(chan kafka.Message, 10)
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 10 * time.Second}
	w := newWorker(reader, store, results, edges, frontier, dlq, ttl, maxDepth, retryMax, retryBase, retryMaxDelay, client, 1, 5*time.Minute, 90*time.Second, commitCh, &wg, nil)
	return w, commitCh, &wg
}

func mustNewTestWorker(reader messageReader, store dedupeStore, results, edges, frontier, dlq resultWriter, ttl time.Duration, maxDepth, retryMax int, retryBase, retryMaxDelay time.Duration) *worker {
	w, _, _ := newTestWorker(reader, store, results, edges, frontier, dlq, ttl, maxDepth, retryMax, retryBase, retryMaxDelay)
	return w
}

func TestDedupeKeyForJobWork(t *testing.T) {
	job := models.CrawlJob{URL: "https://openlibrary.org/works/OL45883W"}
	if got := dedupeKeyForJob(job); got != "visited:work:https://openlibrary.org/works/OL45883W" {
		t.Fatalf("unexpected dedupe key: %s", got)
	}
}

func TestDedupeKeyForJobAuthor(t *testing.T) {
	job := models.CrawlJob{URL: "https://openlibrary.org/authors/OL23919A"}
	if got := dedupeKeyForJob(job); got != "visited:author:https://openlibrary.org/authors/OL23919A" {
		t.Fatalf("unexpected dedupe key: %s", got)
	}
}

func TestDedupeKeyForJobBook(t *testing.T) {
	job := models.CrawlJob{URL: "https://openlibrary.org/books/OL45883M"}
	if got := dedupeKeyForJob(job); got != "visited:book:https://openlibrary.org/books/OL45883M" {
		t.Fatalf("unexpected dedupe key: %s", got)
	}
}

func TestIsSearchURL(t *testing.T) {
	if !isSearchURL("https://openlibrary.org/search.json?q=tolkien") {
		t.Fatal("expected search URL to be detected")
	}
	if isSearchURL("https://openlibrary.org/works/OL45883W") {
		t.Fatal("expected work URL to be non-search")
	}
}

func TestParseDurationValid(t *testing.T) {
	got := common.ParseDuration("2h", 5*time.Minute)
	if got != 2*time.Hour {
		t.Fatalf("expected 2h, got %s", got)
	}
}

func TestParseDurationInvalidUsesFallback(t *testing.T) {
	fallback := 5 * time.Minute
	got := common.ParseDuration("not-a-duration", fallback)
	if got != fallback {
		t.Fatalf("expected fallback %s, got %s", fallback, got)
	}
}

func TestParseIntInvalidUsesFallback(t *testing.T) {
	fallback := 7
	got := common.ParseInt("nope", fallback)
	if got != fallback {
		t.Fatalf("expected fallback %d, got %d", fallback, got)
	}
}

// --- Proxy pool (multi-egress) tests ---

func TestSelectProxyFromPool_EmptyPool(t *testing.T) {
	if got := selectProxyFromPool("", "worker-0"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	if got := selectProxyFromPool("  ,  ", "worker-0"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSelectProxyFromPool_SingleProxy(t *testing.T) {
	pool := "http://proxy:8080"
	got := selectProxyFromPool(pool, "worker-0")
	if got != pool {
		t.Fatalf("expected %q, got %q", pool, got)
	}
	got2 := selectProxyFromPool(pool, "worker-1")
	if got2 != pool {
		t.Fatalf("expected %q, got %q", pool, got2)
	}
}

func TestSelectProxyFromPool_Deterministic(t *testing.T) {
	pool := "http://p0:8080,http://p1:8080,http://p2:8080"
	got := selectProxyFromPool(pool, "relentless-worker-0")
	if got == "" {
		t.Fatal("expected one of pool, got empty")
	}
	valid := map[string]bool{"http://p0:8080": true, "http://p1:8080": true, "http://p2:8080": true}
	if !valid[got] {
		t.Fatalf("got %q not in pool", got)
	}
	// Same hostname must yield same proxy
	got2 := selectProxyFromPool(pool, "relentless-worker-0")
	if got != got2 {
		t.Fatalf("deterministic: expected %q, got %q", got, got2)
	}
}

func TestSelectProxyFromPool_Spread(t *testing.T) {
	pool := "http://a:80,http://b:80"
	seen := make(map[string]bool)
	for _, hostname := range []string{"worker-0", "worker-1", "worker-2", "worker-3", "pod-x", "pod-y"} {
		got := selectProxyFromPool(pool, hostname)
		if got == "" {
			t.Fatalf("hostname %q: expected one of pool, got empty", hostname)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected at least 2 different proxies used across hostnames, got %v", seen)
	}
}

func TestSelectProxyFromPool_TrimSpace(t *testing.T) {
	pool := " http://one:8080 , http://two:8080 "
	got := selectProxyFromPool(pool, "worker-0")
	if got != "http://one:8080" && got != "http://two:8080" {
		t.Fatalf("expected trimmed URL from pool, got %q", got)
	}
}

func TestBuildHTTPClient_NoProxy(t *testing.T) {
	os.Unsetenv("PROXY_URL")
	os.Unsetenv("PROXY_POOL")
	os.Unsetenv("HOSTNAME")
	defer func() {
		os.Unsetenv("PROXY_URL")
		os.Unsetenv("PROXY_POOL")
		os.Unsetenv("HOSTNAME")
	}()
	client := buildHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport for timeouts, got %T", client.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("expected no proxy when no proxy env")
	}
	if client.Timeout != 30*time.Second {
		t.Fatalf("expected total timeout 30s, got %v", client.Timeout)
	}
}

func TestBuildHTTPClient_ProxyURL(t *testing.T) {
	proxyURL := "http://proxy.example:8080"
	os.Setenv("PROXY_URL", proxyURL)
	os.Unsetenv("PROXY_POOL")
	defer func() {
		os.Unsetenv("PROXY_URL")
		os.Unsetenv("PROXY_POOL")
	}()
	client := buildHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("expected Transport with Proxy when PROXY_URL set")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://openlibrary.org/works/OL1.json", nil)
	u, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy(req): %v", err)
	}
	if u == nil || u.String() != proxyURL {
		t.Fatalf("expected proxy %q, got %v", proxyURL, u)
	}
}

func TestBuildHTTPClient_ProxyPool(t *testing.T) {
	pool := "http://p0:8080,http://p1:8080,http://p2:8080"
	os.Unsetenv("PROXY_URL")
	os.Setenv("PROXY_POOL", pool)
	os.Setenv("HOSTNAME", "relentless-worker-0")
	defer func() {
		os.Unsetenv("PROXY_URL")
		os.Unsetenv("PROXY_POOL")
		os.Unsetenv("HOSTNAME")
	}()
	client := buildHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("expected Transport with Proxy when PROXY_POOL set")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://openlibrary.org/works/OL1.json", nil)
	u, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy(req): %v", err)
	}
	if u == nil {
		t.Fatal("expected proxy URL, got nil")
	}
	valid := map[string]bool{"http://p0:8080": true, "http://p1:8080": true, "http://p2:8080": true}
	if !valid[u.String()] {
		t.Fatalf("proxy %q not in pool", u.String())
	}
}

func TestBuildHTTPClient_ProxyURLTakesPrecedence(t *testing.T) {
	os.Setenv("PROXY_URL", "http://single:9090")
	os.Setenv("PROXY_POOL", "http://p0:80,http://p1:80")
	os.Setenv("HOSTNAME", "worker-0")
	defer func() {
		os.Unsetenv("PROXY_URL")
		os.Unsetenv("PROXY_POOL")
		os.Unsetenv("HOSTNAME")
	}()
	client := buildHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("expected Transport with Proxy")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://openlibrary.org/works/OL1.json", nil)
	u, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy(req): %v", err)
	}
	if u == nil || u.String() != "http://single:9090" {
		t.Fatalf("expected PROXY_URL to take precedence, got %v", u)
	}
}

func TestBuildHTTPClient_InvalidProxyURL(t *testing.T) {
	os.Setenv("PROXY_URL", "://invalid")
	os.Unsetenv("PROXY_POOL")
	defer func() {
		os.Unsetenv("PROXY_URL")
		os.Unsetenv("PROXY_POOL")
	}()
	client := buildHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	// Invalid proxy URL is logged but Proxy is not set; Transport still has timeouts.
	if transport.Proxy != nil {
		req, _ := http.NewRequest(http.MethodGet, "https://openlibrary.org/works/OL1.json", nil)
		if u, _ := transport.Proxy(req); u != nil {
			t.Fatalf("expected no proxy for invalid PROXY_URL, got %v", u)
		}
	}
}

func TestBuildHTTPClient_ProxyPoolEmptyHostnameUsesDefault(t *testing.T) {
	os.Unsetenv("PROXY_URL")
	os.Setenv("PROXY_POOL", "http://only:8080")
	os.Unsetenv("HOSTNAME")
	defer func() {
		os.Unsetenv("PROXY_URL")
		os.Unsetenv("PROXY_POOL")
		os.Unsetenv("HOSTNAME")
	}()
	client := buildHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.Proxy == nil {
		t.Fatalf("expected Transport with Proxy when PROXY_POOL set and HOSTNAME unset")
	}
	req, _ := http.NewRequest(http.MethodGet, "https://openlibrary.org/works/OL1.json", nil)
	u, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy(req): %v", err)
	}
	if u == nil || u.String() != "http://only:8080" {
		t.Fatalf("expected single proxy when HOSTNAME unset, got %v", u)
	}
}

func TestHandleSearchParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"docs":[{"key":"/works/OL1W"}]}`))
	}))
	defer server.Close()

	job := models.CrawlJob{URL: server.URL + "/search.json?q=test"}
	client := &http.Client{Timeout: 10 * time.Second}
	if _, err := handleSearch(context.Background(), client, job); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestHandleJobWithRetryStopsAfterMax(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	w := mustNewTestWorker(nil, nil, nil, nil, nil, nil, time.Hour, 5, 2, 0, 0)
	job := models.CrawlJob{URL: server.URL + "/works/OL1W"}
	if _, err := w.handleJobWithRetry(context.Background(), job); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestHandleJobWithRetrySucceedsAfterRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"/works/OL1W","type":{"key":"/type/work"}}`))
	}))
	defer server.Close()

	w := mustNewTestWorker(nil, nil, nil, nil, nil, nil, time.Hour, 5, 2, 0, 0)
	job := models.CrawlJob{URL: server.URL + "/works/OL1W"}
	if _, err := w.handleJobWithRetry(context.Background(), job); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestHandleJobWithRetry_RetriesBeforeFail verifies retries succeed when server returns 500 then 200.
func TestHandleJobWithRetry_RetriesBeforeFail(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		if n < 3 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"/works/OL1W","type":{"key":"/type/work"}}`))
	}))
	defer server.Close()

	w := mustNewTestWorker(nil, nil, nil, nil, nil, nil, time.Hour, 5, 2, 0, 0)
	job := models.CrawlJob{URL: server.URL + "/works/OL1W"}
	node, err := w.handleJobWithRetry(context.Background(), job)
	if err != nil {
		t.Fatalf("expected nil error after retries, got %v", err)
	}
	if node == nil {
		t.Fatal("expected node, got nil")
	}
	if got := atomic.LoadInt32(&callCount); got != 3 {
		t.Fatalf("expected server called 3 times (initial + 2 retries), got %d", got)
	}
}

// TestHandleJobWithRetry_DLQAfterExhaustedRetries verifies exhausted retries produce exactly one DLQ entry.
func TestHandleJobWithRetry_DLQAfterExhaustedRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	store := mocks.NewMockdedupeStore(ctrl)
	dlq := mocks.NewMockResultWriter(ctrl)

	job := models.CrawlJob{
		SessionID: "session-dlq",
		SeedURL:   "https://openlibrary.org/authors/OL9388A",
		URL:       server.URL + "/works/OL999W",
		Depth:     1,
	}
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("failed to marshal job: %v", err)
	}

	store.EXPECT().SetNX(gomock.Any(), "visited:work:"+job.URL, "1", time.Hour).Return(true, nil)
	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)

	var got models.CrawlFailure
	dlq.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			if len(msgs) != 1 {
				t.Fatalf("expected 1 DLQ message, got %d", len(msgs))
			}
			if err := json.Unmarshal(msgs[0].Value, &got); err != nil {
				t.Fatalf("failed to decode CrawlFailure: %v", err)
			}
			return nil
		}).Times(1)

	w, commitCh, wg := newTestWorker(reader, store, nil, nil, nil, dlq, time.Hour, 3, 2, 0, 0)
	commitDone := make(chan struct{})
	go func() {
		m := <-commitCh
		_ = reader.CommitMessages(context.Background(), m)
		close(commitDone)
	}()
	if err := w.processMessage(context.Background(), kafka.Message{Value: payload}); err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}
	wg.Wait()
	<-commitDone // wait for commit coordinator goroutine to call CommitMessages before ctrl.Finish

	if got.SessionID != job.SessionID || got.URL != job.URL || got.Error == "" {
		t.Fatalf("unexpected CrawlFailure: %+v", got)
	}
}

func TestPublishDLQWritesFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	dlq := mocks.NewMockResultWriter(ctrl)
	job := models.CrawlJob{
		SessionID: "session-9",
		SeedURL:   "https://openlibrary.org/books/OL1M",
		URL:       "https://openlibrary.org/works/OL1W",
		Depth:     2,
	}

	var got models.CrawlFailure
	dlq.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			if len(msgs) != 1 {
				t.Fatalf("expected 1 message, got %d", len(msgs))
			}
			if err := json.Unmarshal(msgs[0].Value, &got); err != nil {
				t.Fatalf("failed to decode failure: %v", err)
			}
			return nil
		},
	).Times(1)

	w := mustNewTestWorker(nil, nil, nil, nil, nil, dlq, time.Hour, 5, 0, 0, 0)
	if err := w.publishDLQ(context.Background(), job, errors.New("boom")); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if got.SessionID != job.SessionID || got.URL != job.URL || got.Error == "" {
		t.Fatalf("unexpected failure payload: %+v", got)
	}
}

func TestHandleMetricsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	rec := httptest.NewRecorder()

	handleMetrics(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func resetWorkerMetrics() {
	atomic.StoreUint64(&workerJobsReceived, 0)
	atomic.StoreUint64(&workerJobsSkipped, 0)
	atomic.StoreUint64(&workerJobsSuccess, 0)
	atomic.StoreUint64(&workerJobsFailed, 0)
	atomic.StoreUint64(&fetchLatencySumNs, 0)
	atomic.StoreUint64(&fetchLatencyCount, 0)
	for i := range fetchLatencyCounts {
		atomic.StoreUint64(&fetchLatencyCounts[i], 0)
	}
}

func TestHandleMetricsOK(t *testing.T) {
	resetWorkerMetrics()
	atomic.StoreUint64(&workerJobsReceived, 4)
	atomic.StoreUint64(&workerJobsSkipped, 1)
	atomic.StoreUint64(&workerJobsSuccess, 2)
	atomic.StoreUint64(&workerJobsFailed, 1)
	observeFetchLatency(120 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected metrics body")
	}
	for _, line := range []string{
		"relentless_worker_up 1",
		"relentless_worker_jobs_received_total 4",
		"relentless_worker_jobs_skipped_total 1",
		"relentless_worker_jobs_success_total 2",
		"relentless_worker_jobs_failed_total 1",
		"# TYPE relentless_worker_fetch_latency_seconds histogram",
		"relentless_worker_fetch_latency_seconds_bucket",
		"relentless_worker_fetch_latency_seconds_sum",
		"relentless_worker_fetch_latency_seconds_count",
		"relentless_worker_commit_errors_total",
		"relentless_worker_commit_pending_total",
		"relentless_worker_in_flight",
		"# TYPE relentless_worker_commit_latency_seconds histogram",
		"relentless_worker_commit_latency_seconds_bucket",
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("expected metrics to contain %q", line)
		}
	}
}

func TestEnqueueURLToFronterWritesJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	frontier := mocks.NewMockResultWriter(ctrl)

	job := models.CrawlJob{
		SessionID: "session-1",
		SeedURL:   "https://openlibrary.org/books/OL1M",
		URL:       "https://openlibrary.org/books/OL1M",
		Depth:     2,
	}
	targetURL := "https://openlibrary.org/works/OL1W.json"

	var got models.CrawlJob
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			if len(msgs) != 1 {
				t.Fatalf("expected 1 message, got %d", len(msgs))
			}
			if err := json.Unmarshal(msgs[0].Value, &got); err != nil {
				t.Fatalf("failed to decode job: %v", err)
			}
			return nil
		},
	).Times(1)

	w := mustNewTestWorker(nil, nil, nil, nil, frontier, nil, time.Hour, 5, 0, 0, 0)
	if err := w.enqueueURLToFronter(context.Background(), job, targetURL); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if got.URL != targetURL {
		t.Fatalf("expected url %s, got %s", targetURL, got.URL)
	}
	if got.Depth != job.Depth+1 {
		t.Fatalf("expected depth %d, got %d", job.Depth+1, got.Depth)
	}
	if got.SessionID != job.SessionID || got.SeedURL != job.SeedURL {
		t.Fatalf("unexpected job metadata: %+v", got)
	}
}

func TestEnqueueURLToFronterRespectsMaxDepth(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	frontier := mocks.NewMockResultWriter(ctrl)
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)

	job := models.CrawlJob{
		SessionID: "session-1",
		SeedURL:   "https://openlibrary.org/books/OL1M",
		URL:       "https://openlibrary.org/books/OL1M",
		Depth:     2,
	}

	w := mustNewTestWorker(nil, nil, nil, nil, frontier, nil, time.Hour, 2, 0, 0, 0)
	if err := w.enqueueURLToFronter(context.Background(), job, "https://openlibrary.org/works/OL1W.json"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestPublishEdgeWritesMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	edges := mocks.NewMockResultWriter(ctrl)
	job := models.CrawlJob{SessionID: "session-9"}

	var got models.Edge
	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			if len(msgs) != 1 {
				t.Fatalf("expected 1 message, got %d", len(msgs))
			}
			if err := json.Unmarshal(msgs[0].Value, &got); err != nil {
				t.Fatalf("failed to decode edge: %v", err)
			}
			return nil
		},
	).Times(1)

	w := mustNewTestWorker(nil, nil, nil, edges, nil, nil, time.Hour, 5, 0, 0, 0)
	if err := w.publishEdge(context.Background(), job, "/from", "/to", "relation"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if got.SessionID != job.SessionID || got.From != "/from" || got.To != "/to" || got.Relation != "relation" {
		t.Fatalf("unexpected edge: %+v", got)
	}
}

func TestPublishEdgesAndFrontierSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"docs":[{"key":"/works/OL1W"},{"key":""},{"key":"/works/OL2W"}]}`))
	}))
	defer server.Close()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	edges := mocks.NewMockResultWriter(ctrl)
	frontier := mocks.NewMockResultWriter(ctrl)
	job := models.CrawlJob{SessionID: "session-2", URL: server.URL + "/search.json?q=test"}

	var edgeMsgs []models.Edge
	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			for _, msg := range msgs {
				var edge models.Edge
				if err := json.Unmarshal(msg.Value, &edge); err != nil {
					t.Fatalf("failed to decode edge: %v", err)
				}
				edgeMsgs = append(edgeMsgs, edge)
			}
			return nil
		},
	).Times(2)

	var frontierMsgs []models.CrawlJob
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			for _, msg := range msgs {
				var cj models.CrawlJob
				if err := json.Unmarshal(msg.Value, &cj); err != nil {
					t.Fatalf("failed to decode job: %v", err)
				}
				frontierMsgs = append(frontierMsgs, cj)
			}
			return nil
		},
	).Times(2)

	w := mustNewTestWorker(nil, nil, nil, edges, frontier, nil, time.Hour, 5, 0, 0, 0)
	if err := w.publishEdgesAndFrontier(context.Background(), job, nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(edgeMsgs) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(edgeMsgs))
	}
	if len(frontierMsgs) != 2 {
		t.Fatalf("expected 2 frontier jobs, got %d", len(frontierMsgs))
	}
}

func TestPublishEdgesAndFrontierBook(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	edges := mocks.NewMockResultWriter(ctrl)
	frontier := mocks.NewMockResultWriter(ctrl)
	job := models.CrawlJob{SessionID: "session-3", SeedURL: "https://openlibrary.org/books/OL1M"}
	node := models.BookNode{
		Key:      "/books/OL1M",
		Authors:  []string{"/authors/OL1A"},
		Works:    []string{"/works/OL2W"},
		Subjects: []string{"Time travel"},
	}

	var edgeMsgs []models.Edge
	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			for _, msg := range msgs {
				var edge models.Edge
				if err := json.Unmarshal(msg.Value, &edge); err != nil {
					t.Fatalf("failed to decode edge: %v", err)
				}
				edgeMsgs = append(edgeMsgs, edge)
			}
			return nil
		},
	).Times(3)

	var frontierMsgs []models.CrawlJob
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			for _, msg := range msgs {
				var cj models.CrawlJob
				if err := json.Unmarshal(msg.Value, &cj); err != nil {
					t.Fatalf("failed to decode job: %v", err)
				}
				frontierMsgs = append(frontierMsgs, cj)
			}
			return nil
		},
	).Times(3)

	w := mustNewTestWorker(nil, nil, nil, edges, frontier, nil, time.Hour, 5, 0, 0, 0)
	if err := w.publishEdgesAndFrontier(context.Background(), job, node); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if len(edgeMsgs) != 3 || len(frontierMsgs) != 3 {
		t.Fatalf("expected 3 edges and 3 frontier jobs, got %d edges and %d jobs", len(edgeMsgs), len(frontierMsgs))
	}

	var frontierURLs []string
	for _, cj := range frontierMsgs {
		frontierURLs = append(frontierURLs, cj.URL)
	}
	sort.Strings(frontierURLs)
	expected := []string{
		"https://openlibrary.org/authors/OL1A.json",
		"https://openlibrary.org/search.json?subject=Time+travel",
		"https://openlibrary.org/works/OL2W.json",
	}
	sort.Strings(expected)
	for i, url := range expected {
		if frontierURLs[i] != url {
			t.Fatalf("unexpected frontier url: %s", frontierURLs[i])
		}
	}
}

func TestPublishEdgesAndFrontierAuthor(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	frontier := mocks.NewMockResultWriter(ctrl)
	job := models.CrawlJob{SessionID: "session-4"}
	node := models.AuthorNode{Name: "J.R.R. Tolkien"}

	var frontierMsgs []models.CrawlJob
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			for _, msg := range msgs {
				var cj models.CrawlJob
				if err := json.Unmarshal(msg.Value, &cj); err != nil {
					t.Fatalf("failed to decode job: %v", err)
				}
				frontierMsgs = append(frontierMsgs, cj)
			}
			return nil
		},
	).Times(1)

	w := mustNewTestWorker(nil, nil, nil, nil, frontier, nil, time.Hour, 5, 0, 0, 0)
	if err := w.publishEdgesAndFrontier(context.Background(), job, node); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(frontierMsgs) != 1 {
		t.Fatalf("expected 1 frontier job, got %d", len(frontierMsgs))
	}
}

func TestPublishEdgesAndFrontierWork(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	edges := mocks.NewMockResultWriter(ctrl)
	frontier := mocks.NewMockResultWriter(ctrl)
	job := models.CrawlJob{SessionID: "session-5"}
	node := models.WorkNode{
		Key:      "/works/OL9W",
		Authors:  []string{"/authors/OL1A", "/authors/OL2A"},
		Subjects: []string{"Biography"},
	}

	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			return nil
		},
	).Times(3)
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msgs ...kafka.Message) error {
			return nil
		},
	).Times(3)

	w := mustNewTestWorker(nil, nil, nil, edges, frontier, nil, time.Hour, 5, 0, 0, 0)
	if err := w.publishEdgesAndFrontier(context.Background(), job, node); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestWorkerProcessMessageDeduped(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	store := mocks.NewMockdedupeStore(ctrl)
	results := mocks.NewMockResultWriter(ctrl)
	edges := mocks.NewMockResultWriter(ctrl)
	frontier := mocks.NewMockResultWriter(ctrl)

	job := models.CrawlJob{
		SessionID: "session-1",
		URL:       "https://openlibrary.org/works/OL45883W",
	}
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("failed to marshal job: %v", err)
	}

	store.EXPECT().SetNX(gomock.Any(), "visited:work:"+job.URL, "1", time.Hour).Return(false, nil)
	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)

	results.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	w, commitCh, _ := newTestWorker(reader, store, results, edges, frontier, nil, time.Hour, 3, 0, 0, 0)
	if err := w.processMessage(context.Background(), kafka.Message{Value: payload}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	m := <-commitCh
	_ = reader.CommitMessages(context.Background(), m)
}

func TestWorkerProcessMessageDedupeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	store := mocks.NewMockdedupeStore(ctrl)
	results := mocks.NewMockResultWriter(ctrl)
	edges := mocks.NewMockResultWriter(ctrl)
	frontier := mocks.NewMockResultWriter(ctrl)

	job := models.CrawlJob{
		SessionID: "session-2",
		URL:       "https://openlibrary.org/works/OL45883W",
	}
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("failed to marshal job: %v", err)
	}

	store.EXPECT().SetNX(gomock.Any(), "visited:work:"+job.URL, "1", time.Hour).Return(false, errors.New("redis down"))

	results.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	w := mustNewTestWorker(reader, store, results, edges, frontier, nil, time.Hour, 3, 0, 0, 0)
	if err := w.processMessage(context.Background(), kafka.Message{Value: payload}); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestWorkerProcessMessageInvalidPayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	store := mocks.NewMockdedupeStore(ctrl)
	results := mocks.NewMockResultWriter(ctrl)
	edges := mocks.NewMockResultWriter(ctrl)
	frontier := mocks.NewMockResultWriter(ctrl)

	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)
	store.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	results.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	w, commitCh, _ := newTestWorker(reader, store, results, edges, frontier, nil, time.Hour, 3, 0, 0, 0)
	if err := w.processMessage(context.Background(), kafka.Message{Value: []byte("{invalid")}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	m := <-commitCh
	_ = reader.CommitMessages(context.Background(), m)
}

func TestWorkerProcessMessageDLQOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	store := mocks.NewMockdedupeStore(ctrl)
	dlq := mocks.NewMockResultWriter(ctrl)

	job := models.CrawlJob{
		SessionID: "session-err",
		URL:       server.URL + "/works/OL1W",
	}
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("failed to marshal job: %v", err)
	}

	store.EXPECT().SetNX(gomock.Any(), "visited:work:"+job.URL, "1", time.Hour).Return(true, nil)
	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)
	dlq.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(1)

	w, commitCh, wg := newTestWorker(reader, store, nil, nil, nil, dlq, time.Hour, 3, 1, 0, 0)
	go func() {
		m := <-commitCh
		_ = reader.CommitMessages(context.Background(), m)
	}()
	if err := w.processMessage(context.Background(), kafka.Message{Value: payload}); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	wg.Wait()
}

// TestPublishTimeoutAdvancesCommit verifies that when the publish phase exceeds publishTimeout,
// the worker returns and the deferred commitCh send runs so the partition advances (avoids stuck partition).
// See results/FAILURE_MODES.MD §6 "Stuck Partition: In-Order Commit and Publish Blocking".
func TestPublishTimeoutAdvancesCommit(t *testing.T) {
	// Minimal work JSON so handleJob succeeds and we enter the publish phase.
	workJSON := []byte(`{"key":"/works/OL1W","title":"Test","type":{"key":"/type/work"},"authors":[],"subjects":[]}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(workJSON)
	}))
	defer server.Close()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	store := mocks.NewMockdedupeStore(ctrl)
	results := mocks.NewMockResultWriter(ctrl)
	edges := mocks.NewMockResultWriter(ctrl)
	frontier := mocks.NewMockResultWriter(ctrl)

	job := models.CrawlJob{
		SessionID: "session-pub-timeout",
		URL:       server.URL + "/works/OL1W",
	}
	payload, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}

	store.EXPECT().SetNX(gomock.Any(), "visited:work:"+job.URL, "1", time.Hour).Return(true, nil)
	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)

	// Simulate a stuck Kafka publish: block until context is cancelled (publishTimeout).
	results.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ ...kafka.Message) error {
			<-ctx.Done()
			return ctx.Err()
		},
	).Times(1)
	edges.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)
	frontier.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Times(0)

	publishTimeout := 50 * time.Millisecond
	jobTimeout := 5 * time.Minute
	commitCh := make(chan kafka.Message, 10)
	var wg sync.WaitGroup
	client := &http.Client{Timeout: 10 * time.Second}
	w := newWorker(reader, store, results, edges, frontier, nil, time.Hour, 3, 0, 0, 0, client, 1, jobTimeout, publishTimeout, commitCh, &wg, nil)

	commitDone := make(chan struct{})
	go func() {
		m := <-commitCh
		_ = reader.CommitMessages(context.Background(), m)
		close(commitDone)
	}()

	if err := w.processMessage(context.Background(), kafka.Message{Partition: 0, Offset: 42, Value: payload}); err != nil {
		t.Fatalf("processMessage: %v", err)
	}

	// If publish timeout works, defer runs and we get the message on commitCh within a short time.
	select {
	case <-commitDone:
		// Goroutine received message and called CommitMessages; commit path advanced.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("commitCh not received within 200ms: publish timeout did not advance commit path (partition would be stuck)")
	}
	wg.Wait()
}

// TestCommitCoordinatorRequeuesOnCommitFailure verifies that when CommitMessages fails,
// the coordinator re-queues the message (does not advance nextOffset) so it is retried on the next drain.
// See results/FAILURE_MODES.MD "Worker: commit failure and re-queue".
func TestCommitCoordinatorRequeuesOnCommitFailure(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	commitCh := make(chan kafka.Message, 2)
	coordinator := newCommitCoordinator(reader, commitCh)

	atomic.StoreUint64(&workerCommitErrorsTotal, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go coordinator.run(ctx, &wg)

	msg0 := kafka.Message{Partition: 0, Offset: 0, Value: []byte("a")}
	msg1 := kafka.Message{Partition: 0, Offset: 1, Value: []byte("b")}

	// First commit (offset 0) fails; coordinator re-queues and does not advance nextOffset.
	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(errors.New("commit failed"))
	// Next drain retries offset 0 (succeeds), then commits offset 1 (succeeds).
	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)
	reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).Return(nil)

	commitCh <- msg0
	time.Sleep(50 * time.Millisecond) // allow first drain (commit fail) to complete
	commitCh <- msg1
	time.Sleep(100 * time.Millisecond) // allow second drain (retry + commit offset 1) before close
	close(commitCh)
	wg.Wait()

	if got := atomic.LoadUint64(&workerCommitErrorsTotal); got != 1 {
		t.Fatalf("expected 1 commit error (relentless_worker_commit_errors_total), got %d", got)
	}
}
