package web

import "sync"

// hub fans out live-update messages to connected browsers. Slow clients are
// dropped rather than allowed to block the indexer.
type hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newHub() *hub {
	return &hub{clients: make(map[chan []byte]struct{})}
}

func (h *hub) subscribe() chan []byte {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *hub) broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			// The client is not keeping up; it will resync on reload.
		}
	}
}

func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
