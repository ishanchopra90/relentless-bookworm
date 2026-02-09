package models

import "time"

// CrawlStatus tracks the state of a crawl session.
type CrawlStatus struct {
	SessionID string    `json:"session_id"`
	SeedURL   string    `json:"seed_url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
