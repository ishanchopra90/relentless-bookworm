package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"

	"relentless-bookworm/common"
	"relentless-bookworm/internal/crawler"
	"relentless-bookworm/internal/models"
	"relentless-bookworm/internal/ol"
)

type messageReader = crawler.MessageReader
type resultWriter = crawler.MessageWriter

type dedupeStore interface {
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	Close() error
}

type redisStore struct {
	client *redis.Client
}

func newRedisStore(addr string) *redisStore {
	return &redisStore{client: redis.NewClient(&redis.Options{Addr: addr})}
}

func (s *redisStore) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, value, ttl).Result()
}

func (s *redisStore) Close() error {
	return s.client.Close()
}

type worker struct {
	reader         messageReader
	store          dedupeStore
	resultsWriter  resultWriter
	edgesWriter    resultWriter
	frontierWriter resultWriter
	dlqWriter      resultWriter
	ttl            time.Duration
	maxDepth       int
	retryMax       int
	retryBase      time.Duration
	retryMaxDelay  time.Duration
	client         *http.Client
	concurrentJobs int
	jobTimeout     time.Duration // per-job deadline so one stuck job can't hold a slot forever
	publishTimeout time.Duration // max time for Kafka publish phase so we never block commit path
	commitCh       chan<- kafka.Message
	sem            chan struct{}
	wg             *sync.WaitGroup
	robots         *ol.RobotsRules // nil = no check (e.g. robots fetch failed at startup)
}

// selectProxyFromPool returns one URL from pool (comma-separated) by hashing hostname.
// Used so each pod picks a deterministic proxy for multi-egress. Empty pool or hostname yields "".
func selectProxyFromPool(pool, hostname string) string {
	parts := strings.Split(strings.TrimSpace(pool), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	var valid []string
	for _, p := range parts {
		if p != "" {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return ""
	}
	if hostname == "" {
		hostname = "0"
	}
	h := fnv.New32a()
	h.Write([]byte(hostname))
	idx := int(h.Sum32()) % len(valid)
	if idx < 0 {
		idx = -idx
	}
	return valid[idx]
}

// metricsProxyURL is the proxy URL this worker uses (set at startup for /metrics proxy label).
var metricsProxyURL string

// Open Library HTTP timeouts so a single hung request doesn't hold a worker slot indefinitely.
const (
	openLibraryConnectTimeout  = 10 * time.Second
	openLibraryResponseTimeout = 25 * time.Second // time to first response header
	openLibraryTotalTimeout    = 30 * time.Second // total request (connect + headers + body)
)

// buildHTTPClient returns an http.Client for Open Library fetches. If PROXY_URL is set, uses that
// proxy; if PROXY_POOL is set (comma-separated URLs), picks one by HOSTNAME (e.g. pod name) so
// replicas spread across proxies for multi-egress / rate-limit bypass.
// Transport uses explicit connect and response-header timeouts so hung requests release the slot.
func buildHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: openLibraryConnectTimeout}).DialContext,
		ResponseHeaderTimeout: openLibraryResponseTimeout,
	}
	proxyURL := common.GetEnv("PROXY_URL", "")
	pool := common.GetEnv("PROXY_POOL", "")
	if proxyURL == "" && pool != "" {
		hostname := os.Getenv("HOSTNAME")
		proxyURL = selectProxyFromPool(pool, hostname)
		if proxyURL != "" {
			log.Printf("worker proxy from pool: hostname=%s proxy=%s", hostname, proxyURL)
		}
	}
	metricsProxyURL = proxyURL
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			log.Printf("invalid PROXY_URL/PROXY_POOL: %v", err)
		} else {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   openLibraryTotalTimeout,
	}
}

func newWorker(
	reader messageReader,
	store dedupeStore,
	resultsWriter resultWriter,
	edgesWriter resultWriter,
	frontierWriter resultWriter,
	dlqWriter resultWriter,
	ttl time.Duration,
	maxDepth int,
	retryMax int,
	retryBase time.Duration,
	retryMaxDelay time.Duration,
	client *http.Client,
	concurrentJobs int,
	jobTimeout time.Duration,
	publishTimeout time.Duration,
	commitCh chan<- kafka.Message,
	wg *sync.WaitGroup,
	robots *ol.RobotsRules,
) *worker {
	if concurrentJobs < 1 {
		concurrentJobs = 1
	}
	if jobTimeout <= 0 {
		jobTimeout = 5 * time.Minute
	}
	if publishTimeout <= 0 {
		publishTimeout = 90 * time.Second
	}
	// Cap publish timeout so job context can still cancel the publish phase
	if publishTimeout >= jobTimeout {
		publishTimeout = jobTimeout - time.Minute
		if publishTimeout < 30*time.Second {
			publishTimeout = 30 * time.Second
		}
	}
	sem := make(chan struct{}, concurrentJobs)
	return &worker{
		reader:          reader,
		store:           store,
		resultsWriter:   resultsWriter,
		edgesWriter:     edgesWriter,
		frontierWriter:  frontierWriter,
		dlqWriter:       dlqWriter,
		ttl:             ttl,
		maxDepth:        maxDepth,
		retryMax:        retryMax,
		retryBase:       retryBase,
		retryMaxDelay:   retryMaxDelay,
		client:          client,
		concurrentJobs:  concurrentJobs,
		jobTimeout:      jobTimeout,
		publishTimeout:  publishTimeout,
		commitCh:        commitCh,
		sem:             sem,
		wg:              wg,
		robots:          robots,
	}
}

func main() {
	broker := common.GetEnv("KAFKA_BROKER", "localhost:9092")
	defaultFrontier := common.GetEnv("KAFKA_TOPIC", "relentless.crawl.frontier")
	groupID := common.GetEnv("KAFKA_GROUP_ID", "relentless-worker")
	redisAddr := common.GetEnv("REDIS_ADDR", "localhost:6379")
	dedupeTTL := common.ParseDuration(common.GetEnv("DEDUPE_TTL", "24h"), 24*time.Hour)
	resultsTopic := common.GetEnv("KAFKA_RESULTS_TOPIC", "relentless.crawl.results")
	edgesTopic := common.GetEnv("KAFKA_EDGES_TOPIC", "relentless.graph.edges")
	frontierTopic := common.GetEnv("KAFKA_FRONTIER_TOPIC", defaultFrontier)
	dlqTopic := common.GetEnv("KAFKA_DLQ_TOPIC", "relentless.crawl.dlq")
	maxDepth := common.ParseInt(common.GetEnv("MAX_DEPTH", "3"), 3)
	retryMax := common.ParseInt(common.GetEnv("RETRY_MAX", "3"), 3)
	retryBase := common.ParseDuration(common.GetEnv("RETRY_BASE_DELAY", "200ms"), 200*time.Millisecond)
	retryMaxDelay := common.ParseDuration(common.GetEnv("RETRY_MAX_DELAY", "2s"), 2*time.Second)
	concurrentJobs := common.ParseInt(common.GetEnv("CONCURRENT_JOBS", "5"), 5)
	jobTimeout := common.ParseDuration(common.GetEnv("JOB_TIMEOUT", "5m"), 5*time.Minute)
	publishTimeout := common.ParseDuration(common.GetEnv("PUBLISH_TIMEOUT", "90s"), 90*time.Second)
	metricsAddr := common.GetEnv("METRICS_ADDR", ":9090")

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{broker},
		Topic:   frontierTopic,
		GroupID: groupID,
	})
	defer func() {
		if err := reader.Close(); err != nil {
			log.Printf("failed to close reader: %v", err)
		}
	}()

	redisClient := newRedisStore(redisAddr)
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Printf("failed to close redis client: %v", err)
		}
	}()

	resultsWriter := &kafka.Writer{
		Addr:                   kafka.TCP(broker),
		Topic:                  resultsTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: false,
	}
	defer func() {
		if err := resultsWriter.Close(); err != nil {
			log.Printf("failed to close results writer: %v", err)
		}
	}()

	edgesWriter := &kafka.Writer{
		Addr:                   kafka.TCP(broker),
		Topic:                  edgesTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: false,
	}
	defer func() {
		if err := edgesWriter.Close(); err != nil {
			log.Printf("failed to close edges writer: %v", err)
		}
	}()

	frontierWriter := &kafka.Writer{
		Addr:                   kafka.TCP(broker),
		Topic:                  frontierTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: false,
	}
	defer func() {
		if err := frontierWriter.Close(); err != nil {
			log.Printf("failed to close frontier writer: %v", err)
		}
	}()

	dlqWriter := &kafka.Writer{
		Addr:                   kafka.TCP(broker),
		Topic:                  dlqTopic,
		Balancer:               &kafka.LeastBytes{},
		AllowAutoTopicCreation: false,
	}
	defer func() {
		if err := dlqWriter.Close(); err != nil {
			log.Printf("failed to close dlq writer: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if metricsAddr != "" {
		startMetricsServer(ctx, metricsAddr)
	}

	commitCh := make(chan kafka.Message, concurrentJobs*2)
	coordinator := newCommitCoordinator(reader, commitCh)
	var coordWg sync.WaitGroup
	coordWg.Add(1)
	go coordinator.run(ctx, &coordWg)

	var wg sync.WaitGroup
	httpClient := buildHTTPClient()
	var robots *ol.RobotsRules
	if common.GetEnv("RESPECT_ROBOTS_TXT", "") == "true" || common.GetEnv("RESPECT_ROBOTS_TXT", "") == "1" {
		robotsCtx, robotsCancel := context.WithTimeout(ctx, 15*time.Second)
		robotsBody, err := ol.FetchRobots(robotsCtx, httpClient, "https://openlibrary.org")
		robotsCancel()
		if err != nil {
			log.Printf("robots.txt fetch failed (will allow all paths): %v", err)
		} else {
			robots = ol.ParseRobots(robotsBody, ol.DefaultUserAgent)
			log.Printf("loaded Open Library robots.txt (paths disallowed by * will be skipped)")
		}
	}
	log.Printf("worker consuming topic=%s group=%s broker=%s concurrent_jobs=%d", frontierTopic, groupID, broker, concurrentJobs)
	w := newWorker(
		reader,
		redisClient,
		resultsWriter,
		edgesWriter,
		frontierWriter,
		dlqWriter,
		dedupeTTL,
		maxDepth,
		retryMax,
		retryBase,
		retryMaxDelay,
		httpClient,
		concurrentJobs,
		jobTimeout,
		publishTimeout,
		commitCh,
		&wg,
		robots,
	)
	w.run(ctx)
	wg.Wait()
	close(commitCh)
	coordWg.Wait()
}

// run consumes messages from the frontier topic, dispatches to worker goroutines
// (bounded by semaphore), and routes commits through the coordinator.
func (w *worker) run(ctx context.Context) {
	for {
		msg, err := w.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("fetch error: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if err := w.dispatchMessage(ctx, msg); err != nil {
			log.Printf("message dispatch error: %v", err)
		}
	}
}

// dispatchMessage parses and dedupes synchronously; spawns a goroutine for fetch+publish.
func (w *worker) dispatchMessage(ctx context.Context, msg kafka.Message) error {
	var job models.CrawlJob
	if err := json.Unmarshal(msg.Value, &job); err != nil {
		log.Printf("invalid job payload: %v", err)
		w.commitCh <- msg
		return nil
	}

	atomic.AddUint64(&workerJobsReceived, 1)
	dedupeKey := dedupeKeyForJob(job)
	ok, err := w.store.SetNX(ctx, dedupeKey, "1", w.ttl)
	if err != nil {
		return err
	}
	if !ok {
		atomic.AddUint64(&workerJobsSkipped, 1)
		log.Printf("duplicate job skipped key=%s", dedupeKey)
		w.commitCh <- msg
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case w.sem <- struct{}{}:
	}
	atomic.AddInt64(&workerInFlight, 1)
	w.wg.Add(1)
	go w.processJobAsync(ctx, msg, job)
	return nil
}

// processMessage is an alias for dispatchMessage for backward compatibility (e.g. tests).
func (w *worker) processMessage(ctx context.Context, msg kafka.Message) error {
	return w.dispatchMessage(ctx, msg)
}

// processJobAsync fetches, publishes, and commits; runs in a worker goroutine.
// Uses a per-job context with timeout so one stuck job can't hold the slot forever.
// Defers sending msg to commitCh so the partition advances even on panic or timeout.
func (w *worker) processJobAsync(ctx context.Context, msg kafka.Message, job models.CrawlJob) {
	// Always release slot and signal commit so one bad job doesn't block the partition.
	defer func() {
		atomic.AddInt64(&workerInFlight, -1)
		<-w.sem
		w.wg.Done()
		w.commitCh <- msg
	}()

	jobCtx, cancel := context.WithTimeout(ctx, w.jobTimeout)
	defer cancel()

	log.Printf("received job session=%s url=%s depth=%d partition=%d offset=%d", job.SessionID, job.URL, job.Depth, msg.Partition, msg.Offset)
	node, err := w.handleJobWithRetry(jobCtx, job)
	if err != nil {
		atomic.AddUint64(&workerJobsFailed, 1)
		log.Printf("job handler error: %v", err)
	}

	// Bounded publish phase so a stuck Kafka write never blocks the commit path (avoids stuck partition).
	publishCtx, publishCancel := context.WithTimeout(jobCtx, w.publishTimeout)
	defer publishCancel()
	log.Printf("publish start partition=%d offset=%d", msg.Partition, msg.Offset)

	if err != nil {
		if dlqErr := w.publishDLQ(publishCtx, job, err); dlqErr != nil {
			log.Printf("dlq publish error: %v", dlqErr)
		}
	} else {
		atomic.AddUint64(&workerJobsSuccess, 1)
	}
	if publishCtx.Err() != nil {
		log.Printf("publish timeout partition=%d offset=%d (advancing to avoid stuck partition)", msg.Partition, msg.Offset)
		return
	}
	if err := w.publishResult(publishCtx, job, node); err != nil {
		log.Printf("publish result error: %v", err)
	}
	if publishCtx.Err() != nil {
		log.Printf("publish timeout partition=%d offset=%d (advancing to avoid stuck partition)", msg.Partition, msg.Offset)
		return
	}
	if err := w.publishEdgesAndFrontier(publishCtx, job, node); err != nil {
		log.Printf("edge/frontier error: %v", err)
	}
	if publishCtx.Err() != nil {
		log.Printf("publish timeout partition=%d offset=%d (advancing to avoid stuck partition)", msg.Partition, msg.Offset)
	}
	log.Printf("publish done partition=%d offset=%d", msg.Partition, msg.Offset)
}

func handleJob(ctx context.Context, client *http.Client, job models.CrawlJob, robots *ol.RobotsRules) (models.Node, error) {
	var fetchURL string
	if strings.Contains(job.URL, "/search.json") {
		fetchURL = job.URL
	} else {
		fetchURL = ol.EnsureJSONURL(job.URL)
	}
	path := ol.PathFromURL(fetchURL)
	if robots != nil && !robots.Allowed(path) {
		return nil, fmt.Errorf("robots.txt disallows path %s", path)
	}
	if strings.Contains(job.URL, "/search.json") {
		return handleSearch(ctx, client, job)
	}
	if strings.Contains(job.URL, "/authors/") {
		return crawlAuthor(ctx, client, job)
	}
	return crawlWork(ctx, client, job)
}

func crawlWork(ctx context.Context, client *http.Client, job models.CrawlJob) (models.Node, error) {
	url := ol.EnsureJSONURL(job.URL)
	body, err := fetchJSONWithMetrics(ctx, client, url)
	if err != nil {
		return nil, err
	}
	node, err := ol.ParseNode(body)
	if err != nil {
		return nil, err
	}
	log.Printf("crawlWork session=%s url=%s depth=%d", job.SessionID, url, job.Depth)
	return node, nil
}

func crawlAuthor(ctx context.Context, client *http.Client, job models.CrawlJob) (models.Node, error) {
	url := ol.EnsureJSONURL(job.URL)
	body, err := fetchJSONWithMetrics(ctx, client, url)
	if err != nil {
		return nil, err
	}
	node, err := ol.ParseNode(body)
	if err != nil {
		return nil, err
	}
	log.Printf("crawlAuthor session=%s url=%s depth=%d", job.SessionID, url, job.Depth)
	return node, nil
}

func handleSearch(ctx context.Context, client *http.Client, job models.CrawlJob) (models.Node, error) {
	body, err := fetchJSONWithMetrics(ctx, client, job.URL)
	if err != nil {
		return nil, err
	}
	if _, err := ol.ParseSearchResponse(body); err != nil {
		return nil, err
	}
	return nil, nil
}

func (w *worker) handleJobWithRetry(ctx context.Context, job models.CrawlJob) (models.Node, error) {
	if w.retryMax <= 0 {
		return handleJob(ctx, w.client, job, w.robots)
	}
	delay := w.retryBase
	attempts := 0
	for {
		node, err := handleJob(ctx, w.client, job, w.robots)
		if err == nil {
			return node, nil
		}
		attempts++
		if attempts > w.retryMax {
			return nil, err
		}
		if delay > 0 {
			if w.retryMaxDelay > 0 && delay > w.retryMaxDelay {
				delay = w.retryMaxDelay
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			delay *= 2
		}
	}
}

func (w *worker) publishResult(ctx context.Context, job models.CrawlJob, node models.Node) error {
	if w.resultsWriter == nil || node == nil {
		return nil
	}

	payload, err := models.NewCrawlResult(job, node)
	if err != nil {
		return err
	}

	msg := kafka.Message{
		Key:   []byte(job.SessionID),
		Value: payload,
		Time:  time.Now().UTC(),
	}
	return w.resultsWriter.WriteMessages(ctx, msg)
}

func (w *worker) publishDLQ(ctx context.Context, job models.CrawlJob, err error) error {
	if w.dlqWriter == nil {
		return nil
	}
	payload, err := json.Marshal(models.CrawlFailure{
		SessionID: job.SessionID,
		SeedURL:   job.SeedURL,
		URL:       job.URL,
		Depth:     job.Depth,
		Error:     err.Error(),
		FailedAt:  time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	msg := kafka.Message{
		Key:   []byte(job.SessionID),
		Value: payload,
		Time:  time.Now().UTC(),
	}
	return w.dlqWriter.WriteMessages(ctx, msg)
}

func (w *worker) publishEdgesAndFrontier(ctx context.Context, job models.CrawlJob, node models.Node) error {
	if isSearchURL(job.URL) {
		return w.expandSearch(ctx, job)
	}
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case models.BookNode:
		return w.expandBook(ctx, job, n)
	case models.WorkNode:
		return w.expandWork(ctx, job, n)
	case models.AuthorNode:
		return w.expandAuthor(ctx, job, n)
	default:
		return nil
	}
}

func (w *worker) expandSearch(ctx context.Context, job models.CrawlJob) error {
	body, err := fetchJSONWithMetrics(ctx, w.client, job.URL)
	if err != nil {
		return err
	}
	resp, err := ol.ParseSearchResponse(body)
	if err != nil {
		return err
	}
	for _, doc := range resp.Docs {
		if doc.Key == "" {
			continue
		}
		workURL := ol.EnsureJSONURL("https://openlibrary.org" + doc.Key)
		if err := w.enqueueURLToFronter(ctx, job, workURL); err != nil {
			return err
		}
		if err := w.publishEdge(ctx, job, job.URL, doc.Key, "search_work"); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) expandBook(ctx context.Context, job models.CrawlJob, node models.BookNode) error {
	for _, authorKey := range node.Authors {
		if err := w.publishEdge(ctx, job, node.Key, authorKey, "book_author"); err != nil {
			return err
		}
		if err := w.enqueueURLToFronter(ctx, job, ol.EnsureJSONURL("https://openlibrary.org"+authorKey)); err != nil {
			return err
		}
	}
	for _, workKey := range node.Works {
		if err := w.publishEdge(ctx, job, node.Key, workKey, "book_work"); err != nil {
			return err
		}
		if err := w.enqueueURLToFronter(ctx, job, ol.EnsureJSONURL("https://openlibrary.org"+workKey)); err != nil {
			return err
		}
	}
	for _, subject := range node.Subjects {
		searchURL := ol.SearchSubjectURL(subject)
		if err := w.enqueueURLToFronter(ctx, job, searchURL); err != nil {
			return err
		}
		if err := w.publishEdge(ctx, job, node.Key, "subject:"+subject, "book_subject"); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) expandAuthor(ctx context.Context, job models.CrawlJob, node models.AuthorNode) error {
	if node.Name == "" {
		return nil
	}
	searchURL := ol.SearchAuthorURL(node.Name)
	if err := w.enqueueURLToFronter(ctx, job, searchURL); err != nil {
		return err
	}
	return nil
}

func (w *worker) expandWork(ctx context.Context, job models.CrawlJob, node models.WorkNode) error {
	for _, authorKey := range node.Authors {
		if err := w.publishEdge(ctx, job, node.Key, authorKey, "work_author"); err != nil {
			return err
		}
		if err := w.enqueueURLToFronter(ctx, job, ol.EnsureJSONURL("https://openlibrary.org"+authorKey)); err != nil {
			return err
		}
	}
	for _, subject := range node.Subjects {
		searchURL := ol.SearchSubjectURL(subject)
		if err := w.enqueueURLToFronter(ctx, job, searchURL); err != nil {
			return err
		}
		if err := w.publishEdge(ctx, job, node.Key, "subject:"+subject, "work_subject"); err != nil {
			return err
		}
	}
	return nil
}

func (w *worker) enqueueURLToFronter(ctx context.Context, job models.CrawlJob, url string) error {
	if w.frontierWriter == nil {
		return nil
	}
	if w.maxDepth > 0 && job.Depth >= w.maxDepth {
		return nil
	}
	next := models.CrawlJob{
		SessionID: job.SessionID,
		SeedURL:   job.SeedURL,
		URL:       url,
		Depth:     job.Depth + 1,
		CreatedAt: time.Now().UTC(),
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return err
	}
	msg := kafka.Message{
		Key:   []byte(job.SessionID),
		Value: payload,
		Time:  time.Now().UTC(),
	}
	return w.frontierWriter.WriteMessages(ctx, msg)
}

func (w *worker) publishEdge(ctx context.Context, job models.CrawlJob, from, to, relation string) error {
	if w.edgesWriter == nil {
		return nil
	}
	edge := models.Edge{
		SessionID: job.SessionID,
		From:      from,
		To:        to,
		Relation:  relation,
	}
	payload, err := json.Marshal(edge)
	if err != nil {
		return err
	}
	msg := kafka.Message{
		Key:   []byte(job.SessionID),
		Value: payload,
		Time:  time.Now().UTC(),
	}
	return w.edgesWriter.WriteMessages(ctx, msg)
}

func isSearchURL(url string) bool {
	return strings.Contains(url, "/search.json")
}

func dedupeKeyForJob(job models.CrawlJob) string {
	if strings.Contains(job.URL, "/authors/") {
		return "visited:author:" + job.URL
	}
	if strings.Contains(job.URL, "/books/") {
		return "visited:book:" + job.URL
	}
	return "visited:work:" + job.URL
}
