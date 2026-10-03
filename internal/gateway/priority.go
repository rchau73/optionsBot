package gateway

import "context"

// PriorityQueue holds outbound requests in two lanes. Next always drains the
// high lane first, so stop-loss and kill-switch calls are never starved by
// market-data traffic.
type PriorityQueue struct {
	high chan Request
	low  chan Request
}

func NewPriorityQueue(highBuf, lowBuf int) *PriorityQueue {
	return &PriorityQueue{
		high: make(chan Request, highBuf),
		low:  make(chan Request, lowBuf),
	}
}

// Enqueue adds req to its lane. It blocks while the lane is full and gives up
// when ctx is cancelled, so a stalled connection cannot hang the caller forever.
func (pq *PriorityQueue) Enqueue(ctx context.Context, req Request) error {
	lane := pq.low
	if req.Priority == PriorityHigh {
		lane = pq.high
	}
	select {
	case lane <- req:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Next blocks until a request is available, preferring the high lane.
// It returns false when ctx is cancelled.
func (pq *PriorityQueue) Next(ctx context.Context) (Request, bool) {
	select {
	case req := <-pq.high:
		return req, true
	default:
	}
	select {
	case req := <-pq.high:
		return req, true
	case req := <-pq.low:
		return req, true
	case <-ctx.Done():
		return Request{}, false
	}
}
