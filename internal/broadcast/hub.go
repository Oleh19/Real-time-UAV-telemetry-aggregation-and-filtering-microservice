package broadcast

import (
	"sync"
	"sync/atomic"

	"uavmonitor/internal/telemetry"
)

const DefaultSubscriberBuffer = 256

type subscriber struct {
	ch chan telemetry.Sample
}

type Hub struct {
	bufferSize  int
	mu          sync.RWMutex
	subscribers map[int64]*subscriber
	nextID      int64
	closed      bool
	delivered   atomic.Int64
	dropped     atomic.Int64
}

func NewHub(bufferSize int) *Hub {
	if bufferSize < 1 {
		bufferSize = DefaultSubscriberBuffer
	}
	return &Hub{
		bufferSize:  bufferSize,
		subscribers: make(map[int64]*subscriber),
	}
}

func (h *Hub) Subscribe() (int64, <-chan telemetry.Sample) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan telemetry.Sample, h.bufferSize)
	if h.closed {
		close(ch)
		return 0, ch
	}
	h.nextID++
	id := h.nextID
	h.subscribers[id] = &subscriber{ch: ch}
	return id, ch
}

func (h *Hub) Unsubscribe(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	sub, ok := h.subscribers[id]
	if !ok {
		return
	}
	delete(h.subscribers, id)
	close(sub.ch)
}

func (h *Hub) Broadcast(sample telemetry.Sample) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return
	}
	for _, sub := range h.subscribers {
		select {
		case sub.ch <- sample:
			h.delivered.Add(1)
		default:
			h.dropped.Add(1)
		}
	}
}

func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, sub := range h.subscribers {
		delete(h.subscribers, id)
		close(sub.ch)
	}
}

func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

func (h *Hub) Delivered() int64 {
	return h.delivered.Load()
}

func (h *Hub) Dropped() int64 {
	return h.dropped.Load()
}
