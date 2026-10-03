package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	gw "optionsbot/internal/gateway"
)

// next pulls one request with a short timeout so a broken queue fails fast.
func next(t *testing.T, pq *gw.PriorityQueue) gw.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, ok := pq.Next(ctx)
	if !ok {
		t.Fatal("timeout waiting for a queued request")
	}
	return req
}

func enqueue(t *testing.T, pq *gw.PriorityQueue, priority int) {
	t.Helper()
	if err := pq.Enqueue(context.Background(), gw.Request{Priority: priority}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

func TestPriorityQueue_HighDrainedBeforeLow(t *testing.T) {
	pq := gw.NewPriorityQueue(16, 16)
	for i := 0; i < 3; i++ {
		enqueue(t, pq, gw.PriorityLow)
	}
	enqueue(t, pq, gw.PriorityHigh)

	if req := next(t, pq); req.Priority != gw.PriorityHigh {
		t.Errorf("expected high priority first, got %d", req.Priority)
	}
}

func TestPriorityQueue_FallsBackToLow(t *testing.T) {
	pq := gw.NewPriorityQueue(16, 16)
	enqueue(t, pq, gw.PriorityLow)

	if req := next(t, pq); req.Priority != gw.PriorityLow {
		t.Errorf("expected low priority, got %d", req.Priority)
	}
}

func TestPriorityQueue_MultipleHighBeforeLow(t *testing.T) {
	pq := gw.NewPriorityQueue(16, 16)
	for i := 0; i < 2; i++ {
		enqueue(t, pq, gw.PriorityLow)
		enqueue(t, pq, gw.PriorityHigh)
	}
	for i := 0; i < 2; i++ {
		if req := next(t, pq); req.Priority != gw.PriorityHigh {
			t.Errorf("item %d: expected high priority, got %d", i, req.Priority)
		}
	}
	for i := 0; i < 2; i++ {
		if req := next(t, pq); req.Priority != gw.PriorityLow {
			t.Errorf("item %d: expected low priority after highs, got %d", i, req.Priority)
		}
	}
}

func TestPriorityQueue_NextStopsOnCancel(t *testing.T) {
	pq := gw.NewPriorityQueue(1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := pq.Next(ctx); ok {
		t.Error("Next on an empty queue with a cancelled context should return false")
	}
}

func TestPriorityQueue_EnqueueFullLaneRespectsContext(t *testing.T) {
	pq := gw.NewPriorityQueue(1, 1)
	enqueue(t, pq, gw.PriorityLow) // fills the low lane

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := pq.Enqueue(ctx, gw.Request{Priority: gw.PriorityLow})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("enqueue into a full lane should give up with the context, got %v", err)
	}
}
