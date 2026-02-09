package kafka

import (
	"context"
	"encoding/json"
	"time"

	"github.com/segmentio/kafka-go"

	"relentless-bookworm/internal/models"
)

// JobProducer publishes CrawlJob messages.
type JobProducer interface {
	WriteJob(ctx context.Context, job models.CrawlJob) error
}

type messageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// Producer wraps a Kafka writer for publishing crawl jobs.
type Producer struct {
	writer messageWriter
}

// NewProducer creates a Kafka producer for the given broker and topic.
func NewProducer(broker, topic string) *Producer {
	return &Producer{
		writer: &kafka.Writer{
			Addr:                   kafka.TCP(broker),
			Topic:                  topic,
			Balancer:               &kafka.LeastBytes{},
			AllowAutoTopicCreation: false,
		},
	}
}

// NewProducerWithWriter builds a producer using a custom writer (tests).
func NewProducerWithWriter(writer messageWriter) *Producer {
	return &Producer{writer: writer}
}

// Close shuts down the underlying writer.
func (p *Producer) Close() error {
	return p.writer.Close()
}

// WriteJob publishes a CrawlJob to Kafka.
func (p *Producer) WriteJob(ctx context.Context, job models.CrawlJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}

	msg := kafka.Message{
		Key:   []byte(job.SessionID),
		Value: payload,
		Time:  time.Now().UTC(),
	}

	return p.writer.WriteMessages(ctx, msg)
}
