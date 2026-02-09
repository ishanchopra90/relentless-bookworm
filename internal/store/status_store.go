package store

import (
	"context"

	"relentless-bookworm/internal/models"
)

// StatusStore persists crawl session status.
type StatusStore interface {
	SetStatus(ctx context.Context, status models.CrawlStatus) error
	GetStatus(ctx context.Context, sessionID string) (models.CrawlStatus, bool, error)
}
