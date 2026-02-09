package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/segmentio/kafka-go"

	"relentless-bookworm/common"
	"relentless-bookworm/internal/crawler"
	"relentless-bookworm/internal/graph"
	"relentless-bookworm/internal/models"
)

type graphWriter struct {
	driver graph.DriverSessioner
}

var (
	// Counters for graph-writer throughput and failures exposed on /metrics.
	// results/edges received: messages fetched from Kafka; failed: write errors on writing to Neo4j.
	graphWriterResultsReceived uint64
	graphWriterResultsFailed   uint64
	graphWriterEdgesReceived   uint64
	graphWriterEdgesFailed     uint64
	graphWriterResultsWritten  uint64
	graphWriterEdgesWritten    uint64
)

type neo4jDriver struct {
	driver neo4j.DriverWithContext
}

func (d *neo4jDriver) NewSession(ctx context.Context, config neo4j.SessionConfig) graph.SessionRunner {
	return d.driver.NewSession(ctx, config)
}

func (d *neo4jDriver) Close(ctx context.Context) error {
	return d.driver.Close(ctx)
}

func main() {
	broker := common.GetEnv("KAFKA_BROKER", "localhost:9092")
	resultsTopic := common.GetEnv("KAFKA_RESULTS_TOPIC", "relentless.crawl.results")
	edgesTopic := common.GetEnv("KAFKA_EDGES_TOPIC", "relentless.graph.edges")
	resultsGroup := common.GetEnv("KAFKA_RESULTS_GROUP", "relentless-graph-results")
	edgesGroup := common.GetEnv("KAFKA_EDGES_GROUP", "relentless-graph-edges")
	metricsAddr := common.GetEnv("METRICS_ADDR", ":9091")

	neo4jURI := common.GetEnv("NEO4J_URI", "neo4j://localhost:7687")
	neo4jUser := common.GetEnv("NEO4J_USER", "neo4j")
	neo4jPassword := common.GetEnv("NEO4J_PASSWORD", "neo4j")

	driver, err := neo4j.NewDriverWithContext(neo4jURI, neo4j.BasicAuth(neo4jUser, neo4jPassword, ""))
	if err != nil {
		log.Fatalf("neo4j driver error: %v", err)
	}
	defer func() {
		if err := driver.Close(context.Background()); err != nil {
			log.Printf("neo4j close error: %v", err)
		}
	}()

	writer := &graphWriter{driver: &neo4jDriver{driver: driver}}

	resultsReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{broker},
		Topic:   resultsTopic,
		GroupID: resultsGroup,
	})
	defer func() {
		if err := resultsReader.Close(); err != nil {
			log.Printf("results reader close error: %v", err)
		}
	}()

	edgesReader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: []string{broker},
		Topic:   edgesTopic,
		GroupID: edgesGroup,
	})
	defer func() {
		if err := edgesReader.Close(); err != nil {
			log.Printf("edges reader close error: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if metricsAddr != "" {
		startMetricsServer(ctx, metricsAddr)
	}

	go consumeResults(ctx, resultsReader, writer)
	go consumeEdges(ctx, edgesReader, writer)

	<-ctx.Done()
}

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
		"relentless_graph_writer_up 1\n"+
			"relentless_graph_writer_results_received_total %d\n"+
			"relentless_graph_writer_results_failed_total %d\n"+
			"relentless_graph_writer_edges_received_total %d\n"+
			"relentless_graph_writer_edges_failed_total %d\n"+
			"relentless_graph_writer_results_written_total %d\n"+
			"relentless_graph_writer_edges_written_total %d\n",
		atomic.LoadUint64(&graphWriterResultsReceived),
		atomic.LoadUint64(&graphWriterResultsFailed),
		atomic.LoadUint64(&graphWriterEdgesReceived),
		atomic.LoadUint64(&graphWriterEdgesFailed),
		atomic.LoadUint64(&graphWriterResultsWritten),
		atomic.LoadUint64(&graphWriterEdgesWritten),
	)
	_, _ = w.Write([]byte(body))
}

func consumeResults(ctx context.Context, reader crawler.MessageReader, writer *graphWriter) {
	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("results fetch error: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		atomic.AddUint64(&graphWriterResultsReceived, 1)
		if err := writer.writeResult(ctx, msg.Value); err != nil {
			atomic.AddUint64(&graphWriterResultsFailed, 1)
			log.Printf("results write error: %v", err)
			continue
		}
		atomic.AddUint64(&graphWriterResultsWritten, 1)

		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("results commit error: %v", err)
		}
	}
}

func consumeEdges(ctx context.Context, reader crawler.MessageReader, writer *graphWriter) {
	for {
		msg, err := reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("edges fetch error: %v", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		atomic.AddUint64(&graphWriterEdgesReceived, 1)
		if err := writer.writeEdge(ctx, msg.Value); err != nil {
			atomic.AddUint64(&graphWriterEdgesFailed, 1)
			log.Printf("edges write error: %v", err)
			continue
		}
		atomic.AddUint64(&graphWriterEdgesWritten, 1)

		if err := reader.CommitMessages(ctx, msg); err != nil {
			log.Printf("edges commit error: %v", err)
		}
	}
}

func (w *graphWriter) writeResult(ctx context.Context, payload []byte) error {
	var result models.CrawlResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return err
	}

	switch result.NodeType {
	case models.NodeTypeEdition:
		var node models.BookNode
		if err := decodeNode(result.Node, &node); err != nil {
			return err
		}
		if node.Key == "" {
			return nil
		}
		return w.writeBook(ctx, result.SessionID, node)
	case models.NodeTypeWork:
		var node models.WorkNode
		if err := decodeNode(result.Node, &node); err != nil {
			return err
		}
		if node.Key == "" {
			return nil
		}
		return w.writeWork(ctx, result.SessionID, node)
	case models.NodeTypeAuthor:
		var node models.AuthorNode
		if err := decodeNode(result.Node, &node); err != nil {
			return err
		}
		if node.Key == "" {
			return nil
		}
		return w.writeAuthor(ctx, result.SessionID, node)
	default:
		return nil
	}
}

func (w *graphWriter) writeEdge(ctx context.Context, payload []byte) error {
	var edge models.Edge
	if err := json.Unmarshal(payload, &edge); err != nil {
		return err
	}
	if edge.From == "" || edge.To == "" {
		return nil
	}

	query, params := buildEdgeQuery(edge)
	return w.runWrite(ctx, query, params)
}

func (w *graphWriter) writeBook(ctx context.Context, sessionID string, node models.BookNode) error {
	query, params := buildBookQuery(sessionID, node)
	return w.runWrite(ctx, query, params)
}

func (w *graphWriter) writeWork(ctx context.Context, sessionID string, node models.WorkNode) error {
	query, params := buildWorkQuery(sessionID, node)
	return w.runWrite(ctx, query, params)
}

func (w *graphWriter) writeAuthor(ctx context.Context, sessionID string, node models.AuthorNode) error {
	query, params := buildAuthorQuery(sessionID, node)
	return w.runWrite(ctx, query, params)
}

func (w *graphWriter) runWrite(ctx context.Context, query string, params map[string]any) error {
	session := w.driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer func() {
		if err := session.Close(ctx); err != nil {
			log.Printf("neo4j session close error: %v", err)
		}
	}()

	_, err := session.ExecuteWrite(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
		_, err := tx.Run(ctx, query, params)
		return nil, err
	})
	return err
}

func buildEdgeQuery(edge models.Edge) (string, map[string]any) {
	fromLabel, fromKey, fromProp := nodeLabel(edge.From)
	toLabel, toKey, toProp := nodeLabel(edge.To)
	rel := relationType(edge.Relation)

	query := fmt.Sprintf(
		"MERGE (from:%s {%s: $fromKey}) "+
			"MERGE (to:%s {%s: $toKey}) "+
			"MERGE (from)-[r:%s {session_id: $session_id}]->(to)",
		fromLabel, fromProp,
		toLabel, toProp,
		rel,
	)

	params := map[string]any{
		"fromKey":    fromKey,
		"toKey":      toKey,
		"session_id": edge.SessionID,
	}
	return query, params
}

func buildBookQuery(sessionID string, node models.BookNode) (string, map[string]any) {
	query := "MERGE (b:Book {key: $key}) " +
		"SET b.session_id = $session_id, " +
		"b.title = coalesce($title, b.title), " +
		"b.full_title = coalesce($full_title, b.full_title), " +
		"b.publish_date = coalesce($publish_date, b.publish_date)"
	var title any
	if node.Title != "" {
		title = node.Title
	}
	var fullTitle any
	if node.FullTitle != "" {
		fullTitle = node.FullTitle
	}
	var publishDate any
	if node.PublishDate != "" {
		publishDate = node.PublishDate
	}
	params := map[string]any{
		"key":          node.Key,
		"title":        title,
		"full_title":   fullTitle,
		"publish_date": publishDate,
		"session_id":   sessionID,
	}
	return query, params
}

func buildWorkQuery(sessionID string, node models.WorkNode) (string, map[string]any) {
	query := "MERGE (w:Work {key: $key}) " +
		"SET w.session_id = $session_id, w.title = coalesce($title, w.title)"
	var title any
	if node.Title != "" {
		title = node.Title
	}
	params := map[string]any{
		"key":        node.Key,
		"title":      title,
		"session_id": sessionID,
	}
	return query, params
}

func buildAuthorQuery(sessionID string, node models.AuthorNode) (string, map[string]any) {
	query := "MERGE (a:Author {key: $key}) " +
		"SET a.session_id = $session_id, a.name = coalesce($name, a.name)"
	var name any
	if node.Name != "" {
		name = node.Name
	}
	params := map[string]any{
		"key":        node.Key,
		"name":       name,
		"session_id": sessionID,
	}
	return query, params
}

func nodeLabel(key string) (label string, value string, property string) {
	switch {
	case strings.HasPrefix(key, "/books/"):
		return "Book", key, "key"
	case strings.HasPrefix(key, "/works/"):
		return "Work", key, "key"
	case strings.HasPrefix(key, "/authors/"):
		return "Author", key, "key"
	case strings.HasPrefix(key, "subject:"):
		return "Subject", strings.TrimPrefix(key, "subject:"), "name"
	default:
		return "External", key, "key"
	}
}

func relationType(input string) string {
	switch input {
	case "book_author":
		return "BOOK_AUTHOR"
	case "book_work":
		return "BOOK_WORK"
	case "work_author":
		return "WORK_AUTHOR"
	case "book_subject":
		return "BOOK_SUBJECT"
	case "work_subject":
		return "WORK_SUBJECT"
	case "search_work":
		return "SEARCH_WORK"
	default:
		return strings.ToUpper(strings.ReplaceAll(input, "-", "_"))
	}
}

func decodeNode(input any, target any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
