package crawler

import (
	"context"

	"github.com/segmentio/kafka-go"
)

// MessageReader abstracts kafka.Reader.
type MessageReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// MessageWriter abstracts kafka.Writer.
type MessageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}
