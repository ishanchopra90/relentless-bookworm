package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"relentless-bookworm/common"
	"relentless-bookworm/internal/kafka"
	"relentless-bookworm/internal/models"
	"relentless-bookworm/internal/ol"
	"relentless-bookworm/internal/store"
)

type server struct {
	prod  kafka.JobProducer
	store store.StatusStore
}

func newServer(prod kafka.JobProducer, store store.StatusStore) *server {
	return &server{
		prod:  prod,
		store: store,
	}
}

func main() {
	broker := common.GetEnv("KAFKA_BROKER", "localhost:9092")
	topic := common.GetEnv("KAFKA_TOPIC", "relentless.crawl.frontier")
	redisAddr := common.GetEnv("REDIS_ADDR", "localhost:6379")

	prod := kafka.NewProducer(broker, topic)
	defer func() {
		if err := prod.Close(); err != nil {
			log.Printf("failed to close producer: %v", err)
		}
	}()

	statusStore := store.NewRedisStatusStore(redisAddr, "crawl:status:", 24*time.Hour)
	defer func() {
		if err := statusStore.Close(); err != nil {
			log.Printf("failed to close status store: %v", err)
		}
	}()

	srv := newServer(prod, statusStore)

	mux := http.NewServeMux()
	mux.HandleFunc("/crawl", srv.handleCrawl)
	mux.HandleFunc("/crawl/", srv.handleCrawlStatus)
	mux.HandleFunc("/metrics", srv.handleMetrics)

	addr := ":8080"
	log.Printf("api listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// handleCrawl accepts POST requests to enqueue a crawl job.
//
// Method: POST
// Path:   /crawl?url=...
// Example:
//
//	curl -X POST "http://localhost:8080/crawl?url=https://openlibrary.org/works/OL45883W"
func (s *server) handleCrawl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	seedURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if seedURL == "" {
		http.Error(w, "missing url", http.StatusBadRequest)
		return
	}

	normalizedSeed := normalizeSeedURL(seedURL)
	id := newSessionID()
	createdAt := time.Now().UTC()
	status := models.CrawlStatus{
		SessionID: id,
		SeedURL:   normalizedSeed,
		Status:    "queued",
		CreatedAt: createdAt,
	}

	job := models.CrawlJob{
		SessionID: id,
		SeedURL:   normalizedSeed,
		URL:       normalizedSeed,
		Depth:     0,
		CreatedAt: createdAt,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := s.prod.WriteJob(ctx, job); err != nil {
		http.Error(w, "failed to enqueue job", http.StatusBadGateway)
		return
	}

	if err := s.store.SetStatus(ctx, status); err != nil {
		http.Error(w, "failed to persist status", http.StatusBadGateway)
		return
	}

	writeJSON(w, status, http.StatusAccepted)
}

// handleCrawlStatus returns status for a previously created crawl session.
//
// Method: GET
// Path:   /crawl/{sessionID}
// Example:
//
//	curl "http://localhost:8080/crawl/20260119120000"
func (s *server) handleCrawlStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/crawl/"), "/")
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	status, ok, err := s.store.GetStatus(r.Context(), sessionID)
	if err != nil {
		http.Error(w, "failed to load status", http.StatusBadGateway)
		return
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	writeJSON(w, status, http.StatusOK)
}

// handleMetrics exposes a minimal Prometheus-compatible endpoint.
//
// Method: GET
// Path:   /metrics
// Example:
//
//	curl "http://localhost:8080/metrics"
func (s *server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("relentless_api_up 1\n"))
}
func writeJSON(w http.ResponseWriter, payload any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func newSessionID() string {
	return strings.ReplaceAll(time.Now().UTC().Format("20060102150405.000000000"), ".", "")
}

func normalizeSeedURL(seed string) string {
	if strings.HasPrefix(seed, "http://") || strings.HasPrefix(seed, "https://") {
		return ol.EnsureJSONURL(seed)
	}
	return ol.SearchURL(seed)
}
