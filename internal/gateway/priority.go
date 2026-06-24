package gateway

// PriorityQueue drains the high-priority channel before the low-priority channel.
// This ensures stop-loss and kill-switch calls are never starved by market data polling.
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

func (pq *PriorityQueue) Enqueue(req Request) {
	if req.Priority == PriorityHigh {
		pq.high <- req
	} else {
		pq.low <- req
	}
}

// Next returns the next request, preferring high-priority, falling back to low.
func (pq *PriorityQueue) Next() <-chan Request {
	out := make(chan Request, 1)
	go func() {
		select {
		case req := <-pq.high:
			out <- req
			return
		default:
		}
		select {
		case req := <-pq.high:
			out <- req
		case req := <-pq.low:
			out <- req
		}
	}()
	return out
}

func (pq *PriorityQueue) HighCh() <-chan Request { return pq.high }
func (pq *PriorityQueue) LowCh() <-chan Request  { return pq.low }
