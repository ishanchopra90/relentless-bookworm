package models

// Edge represents a relationship between two nodes.
type Edge struct {
	SessionID string `json:"session_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Relation  string `json:"relation"`
}
