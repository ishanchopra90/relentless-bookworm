package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/segmentio/kafka-go"

	"relentless-bookworm/common"
)

func main() {
	broker := common.GetEnv("KAFKA_BROKER", "localhost:9092")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := kafka.DialContext(ctx, "tcp", broker)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to connect to Kafka at %s: %v\n", broker, err)
		os.Exit(1)
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read metadata: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("connected to Kafka at %s (%d partitions)\n", broker, len(partitions))
}
