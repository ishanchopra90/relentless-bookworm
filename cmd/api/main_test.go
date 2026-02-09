package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/mock/gomock"

	"relentless-bookworm/internal/models"
	"relentless-bookworm/mocks"
)

func newTestServer(t *testing.T, expectWrite bool) (*server, *mocks.MockStatusStore) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	prod := mocks.NewMockJobProducer(ctrl)
	if expectWrite {
		prod.EXPECT().WriteJob(gomock.Any(), gomock.Any()).Return(nil)
	} else {
		prod.EXPECT().WriteJob(gomock.Any(), gomock.Any()).Times(0)
	}

	statusStore := mocks.NewMockStatusStore(ctrl)

	return &server{
		prod:  prod,
		store: statusStore,
	}, statusStore
}

func TestHandleCrawl(t *testing.T) {
	srv, statusStore := newTestServer(t, true)
	statusStore.EXPECT().SetStatus(gomock.Any(), gomock.Any()).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/crawl?url=https://openlibrary.org/works/OL45883W", nil)
	rec := httptest.NewRecorder()
	srv.handleCrawl(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}

	var payload models.CrawlStatus
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.SessionID == "" {
		t.Fatal("expected session id to be set")
	}
	if payload.SeedURL != "https://openlibrary.org/works/OL45883W.json" {
		t.Fatalf("unexpected seed url: %s", payload.SeedURL)
	}
	if payload.Status != "queued" {
		t.Fatalf("unexpected status: %s", payload.Status)
	}
}

func TestHandleCrawlSearchSeed(t *testing.T) {
	srv, statusStore := newTestServer(t, true)
	statusStore.EXPECT().SetStatus(gomock.Any(), gomock.Any()).Return(nil)

	req := httptest.NewRequest(http.MethodPost, "/crawl?url=shakespeare", nil)
	rec := httptest.NewRecorder()
	srv.handleCrawl(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rec.Code)
	}

	var payload models.CrawlStatus
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.SeedURL != "https://openlibrary.org/search.json?q=shakespeare" {
		t.Fatalf("unexpected seed url: %s", payload.SeedURL)
	}
}

func TestHandleCrawlMissingURL(t *testing.T) {
	srv, statusStore := newTestServer(t, false)
	statusStore.EXPECT().SetStatus(gomock.Any(), gomock.Any()).Times(0)

	req := httptest.NewRequest(http.MethodPost, "/crawl", nil)
	rec := httptest.NewRecorder()
	srv.handleCrawl(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleCrawlMethodNotAllowed(t *testing.T) {
	srv, statusStore := newTestServer(t, false)
	statusStore.EXPECT().SetStatus(gomock.Any(), gomock.Any()).Times(0)

	req := httptest.NewRequest(http.MethodGet, "/crawl?url=https://openlibrary.org/works/OL45883W", nil)
	rec := httptest.NewRecorder()
	srv.handleCrawl(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHandleCrawlStatus(t *testing.T) {
	srv, statusStore := newTestServer(t, true)
	statusStore.EXPECT().SetStatus(gomock.Any(), gomock.Any()).Return(nil)

	createReq := httptest.NewRequest(http.MethodPost, "/crawl?url=https://openlibrary.org/works/OL45883W", nil)
	createRec := httptest.NewRecorder()
	srv.handleCrawl(createRec, createReq)

	var created models.CrawlStatus
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	statusStore.EXPECT().
		GetStatus(gomock.Any(), created.SessionID).
		Return(created, true, nil)

	statusReq := httptest.NewRequest(http.MethodGet, "/crawl/"+created.SessionID, nil)
	statusRec := httptest.NewRecorder()
	srv.handleCrawlStatus(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, statusRec.Code)
	}
}

func TestHandleCrawlStatusMatchesCreatedJob(t *testing.T) {
	srv, statusStore := newTestServer(t, true)
	statusStore.EXPECT().SetStatus(gomock.Any(), gomock.Any()).Return(nil)

	createReq := httptest.NewRequest(http.MethodPost, "/crawl?url=https://openlibrary.org/works/OL45883W", nil)
	createRec := httptest.NewRecorder()
	srv.handleCrawl(createRec, createReq)

	var created models.CrawlStatus
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	statusStore.EXPECT().
		GetStatus(gomock.Any(), created.SessionID).
		Return(created, true, nil)

	statusReq := httptest.NewRequest(http.MethodGet, "/crawl/"+created.SessionID, nil)
	statusRec := httptest.NewRecorder()
	srv.handleCrawlStatus(statusRec, statusReq)

	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, statusRec.Code)
	}

	var fetched models.CrawlStatus
	if err := json.NewDecoder(statusRec.Body).Decode(&fetched); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}
	if fetched.SessionID != created.SessionID {
		t.Fatalf("expected session id %s, got %s", created.SessionID, fetched.SessionID)
	}
	if fetched.SeedURL != created.SeedURL {
		t.Fatalf("expected seed url %s, got %s", created.SeedURL, fetched.SeedURL)
	}
	if fetched.Status != created.Status {
		t.Fatalf("expected status %s, got %s", created.Status, fetched.Status)
	}
}

func TestHandleCrawlStatusNotFound(t *testing.T) {
	srv, statusStore := newTestServer(t, false)
	statusStore.EXPECT().GetStatus(gomock.Any(), gomock.Any()).Return(models.CrawlStatus{}, false, nil)

	req := httptest.NewRequest(http.MethodGet, "/crawl/does-not-exist", nil)
	rec := httptest.NewRecorder()
	srv.handleCrawlStatus(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestHandleCrawlStatusMissingID(t *testing.T) {
	srv, statusStore := newTestServer(t, false)
	statusStore.EXPECT().GetStatus(gomock.Any(), gomock.Any()).Times(0)

	req := httptest.NewRequest(http.MethodGet, "/crawl/", nil)
	rec := httptest.NewRecorder()
	srv.handleCrawlStatus(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestHandleMetrics(t *testing.T) {
	srv, _ := newTestServer(t, false)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.handleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	if got := rec.Body.String(); got != "relentless_api_up 1\n" {
		t.Fatalf("unexpected metrics body: %s", got)
	}
}
