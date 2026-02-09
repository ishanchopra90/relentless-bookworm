package models

import "time"

// CrawlFailure captures a failed crawl job for the DLQ.
type CrawlFailure struct {
	SessionID string    `json:"session_id"`
	SeedURL   string    `json:"seed_url"`
	URL       string    `json:"url"`
	Depth     int       `json:"depth"`
	Error     string    `json:"error"`
	FailedAt  time.Time `json:"failed_at"`
}
