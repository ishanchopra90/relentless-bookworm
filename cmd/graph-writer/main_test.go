package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/segmentio/kafka-go"

	"relentless-bookworm/internal/models"
	"relentless-bookworm/mocks"
)

type fakeTx struct {
	query  string
	params map[string]any
}

func (f *fakeTx) Run(_ context.Context, query string, params map[string]any) (neo4j.ResultWithContext, error) {
	f.query = query
	f.params = params
	return nil, nil
}

func newWriterWithQueryCapture(t *testing.T) (*graphWriter, *fakeTx, *bool) {
	t.Helper()

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	driver := mocks.NewMockDriverSessioner(ctrl)
	session := mocks.NewMockSessionRunner(ctrl)
	tx := &fakeTx{}
	called := false

	driver.EXPECT().NewSession(gomock.Any(), gomock.Any()).Return(session).AnyTimes()
	session.EXPECT().Close(gomock.Any()).Return(nil).AnyTimes()
	session.EXPECT().ExecuteWrite(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, work neo4j.ManagedTransactionWork, _ ...func(*neo4j.TransactionConfig)) (any, error) {
			called = true
			return nil, nil
		},
	).AnyTimes()

	return &graphWriter{driver: driver}, tx, &called
}

func resetGraphWriterMetrics() {
	atomic.StoreUint64(&graphWriterResultsReceived, 0)
	atomic.StoreUint64(&graphWriterResultsFailed, 0)
	atomic.StoreUint64(&graphWriterEdgesReceived, 0)
	atomic.StoreUint64(&graphWriterEdgesFailed, 0)
	atomic.StoreUint64(&graphWriterResultsWritten, 0)
	atomic.StoreUint64(&graphWriterEdgesWritten, 0)
}

func TestNodeLabel(t *testing.T) {
	label, value, prop := nodeLabel("/books/OL1M")
	if label != "Book" || value != "/books/OL1M" || prop != "key" {
		t.Fatalf("unexpected book label: %s %s %s", label, value, prop)
	}

	label, value, prop = nodeLabel("subject:Time travel")
	if label != "Subject" || value != "Time travel" || prop != "name" {
		t.Fatalf("unexpected subject label: %s %s %s", label, value, prop)
	}
}

func TestRelationType(t *testing.T) {
	if got := relationType("book_author"); got != "BOOK_AUTHOR" {
		t.Fatalf("unexpected relation: %s", got)
	}
	if got := relationType("custom-edge"); got != "CUSTOM_EDGE" {
		t.Fatalf("unexpected relation: %s", got)
	}
}

func TestBuildQueries(t *testing.T) {
	edge := models.Edge{SessionID: "s1", From: "/books/OL1M", To: "/authors/OL1A", Relation: "book_author"}
	query, params := buildEdgeQuery(edge)
	if query == "" || params["fromKey"] != edge.From || params["toKey"] != edge.To {
		t.Fatalf("unexpected edge query/params: %s %+v", query, params)
	}

	bookQuery, bookParams := buildBookQuery("s1", models.BookNode{Key: "/books/OL1M", Title: "T"})
	if bookQuery == "" || bookParams["key"] != "/books/OL1M" {
		t.Fatalf("unexpected book query/params: %s %+v", bookQuery, bookParams)
	}
	if !strings.Contains(bookQuery, "coalesce") || bookParams["title"] != "T" {
		t.Fatalf("unexpected book query/params: %s %+v", bookQuery, bookParams)
	}

	workQuery, workParams := buildWorkQuery("s1", models.WorkNode{Key: "/works/OL1W", Title: "W"})
	if workQuery == "" || workParams["key"] != "/works/OL1W" {
		t.Fatalf("unexpected work query/params: %s %+v", workQuery, workParams)
	}
	if !strings.Contains(workQuery, "coalesce") || workParams["title"] != "W" {
		t.Fatalf("unexpected work query/params: %s %+v", workQuery, workParams)
	}

	authorQuery, authorParams := buildAuthorQuery("s1", models.AuthorNode{Key: "/authors/OL1A", Name: "A"})
	if authorQuery == "" || authorParams["key"] != "/authors/OL1A" {
		t.Fatalf("unexpected author query/params: %s %+v", authorQuery, authorParams)
	}
	if !strings.Contains(authorQuery, "coalesce") || authorParams["name"] != "A" {
		t.Fatalf("unexpected author query/params: %s %+v", authorQuery, authorParams)
	}
}

func TestDecodeNode(t *testing.T) {
	input := map[string]any{"key": "/books/OL1M", "title": "Test"}
	var node models.BookNode
	if err := decodeNode(input, &node); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if node.Key != "/books/OL1M" || node.Title != "Test" {
		t.Fatalf("unexpected node: %+v", node)
	}
}

func TestWriteEdgeBuildsQuery(t *testing.T) {
	writer, _, called := newWriterWithQueryCapture(t)
	edge := models.Edge{
		SessionID: "s1",
		From:      "/books/OL1M",
		To:        "/authors/OL1A",
		Relation:  "book_author",
	}
	payload, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if err := writer.writeEdge(context.Background(), payload); err != nil {
		t.Fatalf("write edge error: %v", err)
	}
	if !*called {
		t.Fatal("expected execute write call")
	}
}

func TestWriteResultBook(t *testing.T) {
	writer, _, called := newWriterWithQueryCapture(t)
	result := models.CrawlResult{
		SessionID: "s1",
		NodeType:  models.NodeTypeEdition,
		Node:      models.BookNode{Key: "/books/OL1M", Title: "Title"},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if err := writer.writeResult(context.Background(), payload); err != nil {
		t.Fatalf("write result error: %v", err)
	}
	if !*called {
		t.Fatal("expected execute write call")
	}
}

func TestWriteResultWork(t *testing.T) {
	writer, _, called := newWriterWithQueryCapture(t)
	result := models.CrawlResult{
		SessionID: "s1",
		NodeType:  models.NodeTypeWork,
		Node:      models.WorkNode{Key: "/works/OL1W", Title: "Work"},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if err := writer.writeResult(context.Background(), payload); err != nil {
		t.Fatalf("write result error: %v", err)
	}
	if !*called {
		t.Fatal("expected execute write call")
	}
}

func TestWriteResultAuthor(t *testing.T) {
	writer, _, called := newWriterWithQueryCapture(t)
	result := models.CrawlResult{
		SessionID: "s1",
		NodeType:  models.NodeTypeAuthor,
		Node:      models.AuthorNode{Key: "/authors/OL1A", Name: "Author"},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if err := writer.writeResult(context.Background(), payload); err != nil {
		t.Fatalf("write result error: %v", err)
	}
	if !*called {
		t.Fatal("expected execute write call")
	}
}

func TestWriteResultSkipsEmptyKey(t *testing.T) {
	writer, _, called := newWriterWithQueryCapture(t)
	result := models.CrawlResult{
		SessionID: "s1",
		NodeType:  models.NodeTypeEdition,
		Node:      models.BookNode{Key: ""},
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	if err := writer.writeResult(context.Background(), payload); err != nil {
		t.Fatalf("write result error: %v", err)
	}
	if *called {
		t.Fatal("expected no write call")
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

func TestHandleMetricsOK(t *testing.T) {
	resetGraphWriterMetrics()
	atomic.StoreUint64(&graphWriterResultsReceived, 2)
	atomic.StoreUint64(&graphWriterResultsFailed, 1)
	atomic.StoreUint64(&graphWriterEdgesReceived, 3)
	atomic.StoreUint64(&graphWriterEdgesFailed, 1)
	atomic.StoreUint64(&graphWriterResultsWritten, 2)
	atomic.StoreUint64(&graphWriterEdgesWritten, 3)
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
		"relentless_graph_writer_up 1",
		"relentless_graph_writer_results_received_total 2",
		"relentless_graph_writer_results_failed_total 1",
		"relentless_graph_writer_edges_received_total 3",
		"relentless_graph_writer_edges_failed_total 1",
		"relentless_graph_writer_results_written_total 2",
		"relentless_graph_writer_edges_written_total 3",
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("expected metrics to contain %q", line)
		}
	}
}

func TestConsumeResultsCommitsOnSuccess(t *testing.T) {
	resetGraphWriterMetrics()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	writer, _, called := newWriterWithQueryCapture(t)

	payload, err := json.Marshal(models.CrawlResult{
		SessionID: "s1",
		NodeType:  models.NodeTypeEdition,
		Node:      models.BookNode{Key: "/books/OL1M", Title: "Title"},
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gomock.InOrder(
		reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{Value: payload}, nil),
		reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, ...kafka.Message) error {
				cancel()
				return nil
			},
		),
		reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{}, context.Canceled),
	)

	consumeResults(ctx, reader, writer)

	if !*called {
		t.Fatal("expected write to be called")
	}
	if got := atomic.LoadUint64(&graphWriterResultsWritten); got != 1 {
		t.Fatalf("expected results written to be 1, got %d", got)
	}
}

func TestConsumeEdgesCommitsOnSuccess(t *testing.T) {
	resetGraphWriterMetrics()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	reader := mocks.NewMockMessageReader(ctrl)
	writer, _, called := newWriterWithQueryCapture(t)

	payload, err := json.Marshal(models.Edge{
		SessionID: "s1",
		From:      "/books/OL1M",
		To:        "/authors/OL1A",
		Relation:  "book_author",
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gomock.InOrder(
		reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{Value: payload}, nil),
		reader.EXPECT().CommitMessages(gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, ...kafka.Message) error {
				cancel()
				return nil
			},
		),
		reader.EXPECT().FetchMessage(gomock.Any()).Return(kafka.Message{}, context.Canceled),
	)

	consumeEdges(ctx, reader, writer)

	if !*called {
		t.Fatal("expected write to be called")
	}
	if got := atomic.LoadUint64(&graphWriterEdgesWritten); got != 1 {
		t.Fatalf("expected edges written to be 1, got %d", got)
	}
}
