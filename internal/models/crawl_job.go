package models

import "time"

// CrawlJob represents a unit of work for the crawler frontier.
type CrawlJob struct {
	SessionID string    `json:"session_id"`
	SeedURL   string    `json:"seed_url"`
	URL       string    `json:"url"`
	Depth     int       `json:"depth"`
	CreatedAt time.Time `json:"created_at"`
}
