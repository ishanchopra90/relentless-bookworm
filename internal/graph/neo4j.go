package graph

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// SessionRunner abstracts neo4j.SessionWithContext.
type SessionRunner interface {
	ExecuteWrite(ctx context.Context, work neo4j.ManagedTransactionWork, configurers ...func(*neo4j.TransactionConfig)) (any, error)
	Close(ctx context.Context) error
}

// DriverSessioner abstracts neo4j.DriverWithContext.
type DriverSessioner interface {
	NewSession(ctx context.Context, config neo4j.SessionConfig) SessionRunner
	Close(ctx context.Context) error
}
