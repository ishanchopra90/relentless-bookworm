package kafka_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	kgo "github.com/segmentio/kafka-go"

	rkafka "relentless-bookworm/internal/kafka"
	"relentless-bookworm/internal/models"
	"relentless-bookworm/mocks"
)

func TestProducerWriteJob(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	writer := mocks.NewMockMessageWriter(ctrl)
	prod := rkafka.NewProducerWithWriter(writer)

	job := models.CrawlJob{
		SessionID: "session-123",
		SeedURL:   "https://openlibrary.org/works/OL45883W",
		URL:       "https://openlibrary.org/works/OL45883W",
		Depth:     0,
		CreatedAt: time.Unix(0, 0).UTC(),
	}

	writer.EXPECT().
		WriteMessages(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msgs ...kgo.Message) error {
			if len(msgs) != 1 {
				t.Fatalf("expected 1 message, got %d", len(msgs))
			}
			if string(msgs[0].Key) != job.SessionID {
				t.Fatalf("unexpected message key: %s", string(msgs[0].Key))
			}

			var got models.CrawlJob
			if err := json.Unmarshal(msgs[0].Value, &got); err != nil {
				t.Fatalf("failed to decode message: %v", err)
			}
			if got.SessionID != job.SessionID || got.SeedURL != job.SeedURL || got.URL != job.URL || got.Depth != job.Depth {
				t.Fatalf("unexpected job payload: %+v", got)
			}
			return nil
		})

	if err := prod.WriteJob(context.Background(), job); err != nil {
		t.Fatalf("WriteJob returned error: %v", err)
	}
}

func TestProducerWriteJobError(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	writer := mocks.NewMockMessageWriter(ctrl)
	prod := rkafka.NewProducerWithWriter(writer)

	job := models.CrawlJob{SessionID: "session-err"}
	writer.EXPECT().WriteMessages(gomock.Any(), gomock.Any()).Return(errors.New("write failed"))
	if err := prod.WriteJob(context.Background(), job); err == nil {
		t.Fatal("expected error, got nil")
	}
}
