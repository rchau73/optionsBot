package tests

import (
	"testing"
	"time"

	gw "optionsbot/internal/gateway"
)

func TestPriorityQueue_HighDrainedBeforeLow(t *testing.T) {
	pq := gw.NewPriorityQueue(16, 16)

	// Enqueue several low-priority items
	for i := 0; i < 3; i++ {
		pq.Enqueue(gw.Request{Priority: gw.PriorityLow})
	}
	// Enqueue one high-priority item
	pq.Enqueue(gw.Request{Priority: gw.PriorityHigh})

	// The first item drained should be the high-priority one
	select {
	case req := <-pq.Next():
		if req.Priority != gw.PriorityHigh {
			t.Errorf("expected high priority, got %d", req.Priority)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for high-priority item")
	}
}

func TestPriorityQueue_FallsBackToLow(t *testing.T) {
	pq := gw.NewPriorityQueue(16, 16)

	pq.Enqueue(gw.Request{Priority: gw.PriorityLow})

	select {
	case req := <-pq.Next():
		if req.Priority != gw.PriorityLow {
			t.Errorf("expected low priority, got %d", req.Priority)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for low-priority item")
	}
}

func TestPriorityQueue_MultipleHighBeforeLow(t *testing.T) {
	pq := gw.NewPriorityQueue(16, 16)

	// Enqueue alternating: low, high, low, high
	for i := 0; i < 2; i++ {
		pq.Enqueue(gw.Request{Priority: gw.PriorityLow})
		pq.Enqueue(gw.Request{Priority: gw.PriorityHigh})
	}

	// Drain 2 items — both should be high priority
	for i := 0; i < 2; i++ {
		select {
		case req := <-pq.Next():
			if req.Priority != gw.PriorityHigh {
				t.Errorf("item %d: expected high priority, got %d", i, req.Priority)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout on item %d", i)
		}
	}
}
