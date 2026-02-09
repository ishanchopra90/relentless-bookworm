package models

import "encoding/json"

// CrawlResult is the payload written to the results topic.
type CrawlResult struct {
	SessionID string   `json:"session_id"`
	URL       string   `json:"url"`
	NodeType  NodeType `json:"node_type"`
	Node      any      `json:"node"`
}

// NewCrawlResult marshals a crawl result payload.
func NewCrawlResult(job CrawlJob, node Node) ([]byte, error) {
	result := CrawlResult{
		SessionID: job.SessionID,
		URL:       job.URL,
		NodeType:  node.NodeType(),
	}

	switch n := node.(type) {
	case BookNode:
		result.Node = n
	case WorkNode:
		result.Node = n
	case AuthorNode:
		result.Node = n
	default:
		return nil, nil
	}

	return json.Marshal(result)
}
