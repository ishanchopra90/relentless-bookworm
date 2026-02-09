package main

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"

	"relentless-bookworm/internal/crawler"
)

// commitCoordinator buffers completed messages per partition and commits in offset order.
type commitCoordinator struct {
	reader     crawler.MessageReader           // Kafka reader used to commit offsets
	commitCh   <-chan kafka.Message            // receives completed messages from workers
	nextOffset map[int]int64                   // per partition: next offset we expect to commit
	pending    map[int]map[int64]kafka.Message // per partition: buffered messages keyed by offset
	mu         sync.Mutex                      // protects nextOffset and pending
}

// newCommitCoordinator creates a coordinator that receives completed messages on commitCh
// and commits them to the reader in per-partition offset order.
func newCommitCoordinator(reader crawler.MessageReader, commitCh <-chan kafka.Message) *commitCoordinator {
	return &commitCoordinator{
		reader:     reader,
		commitCh:   commitCh,
		nextOffset: make(map[int]int64),
		pending:    make(map[int]map[int64]kafka.Message),
	}
}

// run is the main loop: receives messages from commitCh, enqueues them, and drains
// contiguous offsets per partition. Exits on ctx cancellation or when commitCh is closed.
// Calls wg.Done() when finished.
func (c *commitCoordinator) run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			c.flush(ctx)
			return
		case msg, ok := <-c.commitCh:
			if !ok {
				c.flush(ctx)
				return
			}
			c.enqueue(msg)
			c.drain(ctx, msg.Partition)
		}
	}
}

// enqueue adds a completed message to the buffer for its partition.
// Initializes nextOffset for the partition if this is the first message seen.
func (c *commitCoordinator) enqueue(msg kafka.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := msg.Partition
	off := msg.Offset
	if c.pending[p] == nil {
		c.pending[p] = make(map[int64]kafka.Message)
	}
	c.pending[p][off] = msg
	atomic.AddInt64(&workerCommitPendingTotal, 1)
	if _, exists := c.nextOffset[p]; !exists {
		c.nextOffset[p] = off
	}
}

// commitNext commits the next contiguous message for the given partition.
// Caller must hold c.mu. Returns true if a message was committed, false if none pending.
// Releases lock during CommitMessages (blocking I/O) and re-acquires before returning.
// On commit failure we do not advance nextOffset so the same message can be retried on a later drain.
func (c *commitCoordinator) commitNext(ctx context.Context, partition int, logPrefix string) bool {
	next := c.nextOffset[partition]
	m, ok := c.pending[partition][next]
	if !ok {
		return false
	}
	delete(c.pending[partition], next)
	atomic.AddInt64(&workerCommitPendingTotal, -1)
	c.mu.Unlock()
	start := time.Now()
	err := c.reader.CommitMessages(ctx, m)
	observeCommitLatency(time.Since(start))
	c.mu.Lock()
	if err != nil {
		atomic.AddUint64(&workerCommitErrorsTotal, 1)
		log.Printf("%s partition=%d offset=%d: %v", logPrefix, partition, next, err)
		// Re-queue so we retry on next drain; do not advance nextOffset.
		c.pending[partition][next] = m
		atomic.AddInt64(&workerCommitPendingTotal, 1)
		return false // stop drain so we don't hammer failing commit
	}
	c.nextOffset[partition] = next + 1
	return true
}

// drain commits all contiguous offsets for the given partition, starting from nextOffset.
// Stops when the next expected offset is not yet in the buffer.
func (c *commitCoordinator) drain(ctx context.Context, partition int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.commitNext(ctx, partition, "commit error") {
	}
}

// flush commits any remaining buffered messages on shutdown.
// Called when ctx is cancelled or commitCh is closed.
func (c *commitCoordinator) flush(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p := range c.pending {
		for c.commitNext(ctx, p, "commit flush error") {
		}
	}
}
