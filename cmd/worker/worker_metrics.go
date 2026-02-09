package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"relentless-bookworm/internal/ol"
)

var (
	// Counters for worker crawl activity exposed on /metrics.
	// received: jobs pulled from Kafka; skipped: deduped; success/failed: handler outcome.
	workerJobsReceived uint64
	workerJobsSkipped  uint64
	workerJobsSuccess  uint64
	workerJobsFailed   uint64

	// Histogram buckets for Open Library fetch latency (seconds).
	// Buckets define upper bounds for histogram counts; the +Inf bucket is implicit.
	fetchLatencyBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5}
	// Counts per bucket; last slot holds the +Inf bucket.
	fetchLatencyCounts = make([]uint64, len(fetchLatencyBuckets)+1)
	// Sum and count are used by Prometheus histogram quantiles.
	fetchLatencySumNs uint64
	fetchLatencyCount uint64

	// Open Library rate limit (HTTP 429) hits; one increment per fetch that returned 429.
	workerRateLimitHitsTotal uint64

	// Worker V2 (concurrent + commit coordinator) observability.
	workerCommitErrorsTotal  uint64 // counter: Kafka CommitMessages failures; detect commit issues
	workerCommitPendingTotal int64  // gauge: messages buffered in coordinator awaiting commit; monitor backlog
	workerInFlight           int64  // gauge: jobs currently being processed (semaphore slots in use); confirm parallelism
	// Histogram for Kafka commit latency (seconds). Buckets: upper bounds; +Inf implicit.
	commitLatencyBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}
	commitLatencyCounts  = make([]uint64, len(commitLatencyBuckets)+1) // per-bucket counts; last slot = +Inf
	commitLatencySumNs   uint64                                        // sum of observed durations (ns); for quantiles
	commitLatencyCount   uint64                                        // total observations; for quantiles
)

func startMetricsServer(ctx context.Context, addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", handleMetrics)

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("metrics shutdown error: %v", err)
		}
	}()

	go func() {
		log.Printf("metrics listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("metrics server error: %v", err)
		}
	}()
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	body := fmt.Sprintf(
		"relentless_worker_up 1\n"+
			"relentless_worker_jobs_received_total %d\n"+
			"relentless_worker_jobs_skipped_total %d\n"+
			"relentless_worker_jobs_success_total %d\n"+
			"relentless_worker_jobs_failed_total %d\n",
		atomic.LoadUint64(&workerJobsReceived),
		atomic.LoadUint64(&workerJobsSkipped),
		atomic.LoadUint64(&workerJobsSuccess),
		atomic.LoadUint64(&workerJobsFailed),
	)
	if metricsProxyURL != "" {
		// Proxy pool: segment fetch latency / failures by proxy in Grafana (join by instance/pod).
		body += "# HELP relentless_worker_proxy_info Proxy URL this worker uses (1 when set).\n"
		body += "# TYPE relentless_worker_proxy_info gauge\n"
		body += fmt.Sprintf("relentless_worker_proxy_info{proxy=%q} 1\n", escapeMetricLabel(metricsProxyURL))
	}
	var histogram strings.Builder
	histogram.WriteString("# HELP relentless_worker_fetch_latency_seconds Open Library fetch latency.\n")
	histogram.WriteString("# TYPE relentless_worker_fetch_latency_seconds histogram\n")
	appendHistogram(&histogram, "relentless_worker_fetch_latency_seconds", fetchLatencyBuckets,
		fetchLatencyCounts, &fetchLatencySumNs, &fetchLatencyCount, "%.2f")

	// Worker V2 metrics
	body += "# HELP relentless_worker_rate_limit_hits_total Open Library HTTP 429 (rate limit) responses.\n"
	body += "# TYPE relentless_worker_rate_limit_hits_total counter\n"
	body += fmt.Sprintf(
		"relentless_worker_rate_limit_hits_total %d\n"+
			"relentless_worker_commit_errors_total %d\n"+
			"relentless_worker_commit_pending_total %d\n"+
			"relentless_worker_in_flight %d\n",
		atomic.LoadUint64(&workerRateLimitHitsTotal),
		atomic.LoadUint64(&workerCommitErrorsTotal),
		atomic.LoadInt64(&workerCommitPendingTotal),
		atomic.LoadInt64(&workerInFlight),
	)
	var commitHist strings.Builder
	commitHist.WriteString("# HELP relentless_worker_commit_latency_seconds Kafka commit latency.\n")
	commitHist.WriteString("# TYPE relentless_worker_commit_latency_seconds histogram\n")
	appendHistogram(&commitHist, "relentless_worker_commit_latency_seconds", commitLatencyBuckets,
		commitLatencyCounts, &commitLatencySumNs, &commitLatencyCount, "%.3f")

	_, _ = w.Write([]byte(body + histogram.String() + commitHist.String()))
}

// escapeMetricLabel escapes backslash and double quote for Prometheus label values.
func escapeMetricLabel(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "\"", "\\\"")
}

// appendHistogram writes a Prometheus histogram (buckets, +Inf, sum, count) to sb.
// counts must have len(buckets)+1 elements; leFmt formats bucket bounds (e.g. "%.2f").
func appendHistogram(sb *strings.Builder, name string, buckets []float64, counts []uint64, sumNs, count *uint64, leFmt string) {
	var cumulative uint64
	for i, bound := range buckets {
		cumulative += atomic.LoadUint64(&counts[i])
		sb.WriteString(fmt.Sprintf("%s_bucket{le=\"%s\"} %d\n", name, fmt.Sprintf(leFmt, bound), cumulative))
	}
	cumulative += atomic.LoadUint64(&counts[len(buckets)])
	sb.WriteString(fmt.Sprintf("%s_bucket{le=\"+Inf\"} %d\n", name, cumulative))
	sumSeconds := float64(atomic.LoadUint64(sumNs)) / float64(time.Second)
	sb.WriteString(fmt.Sprintf("%s_sum %.6f\n", name, sumSeconds))
	sb.WriteString(fmt.Sprintf("%s_count %d\n", name, atomic.LoadUint64(count)))
}

// fetchJSONWithMetrics wraps Open Library fetches to record latency and rate-limit hits (429).
// client may use a proxy (PROXY_URL or PROXY_POOL) for multi-egress / rate-limit bypass.
func fetchJSONWithMetrics(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	start := time.Now()
	body, err := ol.FetchJSONWithClient(ctx, client, url)
	observeFetchLatency(time.Since(start))
	if err != nil && strings.Contains(err.Error(), "unexpected status 429") {
		atomic.AddUint64(&workerRateLimitHitsTotal, 1)
	}
	return body, err
}

// observeFetchLatency updates a manual Prometheus histogram.
func observeFetchLatency(duration time.Duration) {
	if duration <= 0 {
		return
	}
	seconds := duration.Seconds()
	bucketIndex := len(fetchLatencyBuckets)
	for i, bound := range fetchLatencyBuckets {
		if seconds <= bound {
			bucketIndex = i
			break
		}
	}
	atomic.AddUint64(&fetchLatencyCounts[bucketIndex], 1)
	atomic.AddUint64(&fetchLatencySumNs, uint64(duration.Nanoseconds()))
	atomic.AddUint64(&fetchLatencyCount, 1)
}

// observeCommitLatency updates the Kafka commit latency histogram.
func observeCommitLatency(duration time.Duration) {
	if duration <= 0 {
		return
	}
	seconds := duration.Seconds()
	bucketIndex := len(commitLatencyBuckets)
	for i, bound := range commitLatencyBuckets {
		if seconds <= bound {
			bucketIndex = i
			break
		}
	}
	atomic.AddUint64(&commitLatencyCounts[bucketIndex], 1)
	atomic.AddUint64(&commitLatencySumNs, uint64(duration.Nanoseconds()))
	atomic.AddUint64(&commitLatencyCount, 1)
}
