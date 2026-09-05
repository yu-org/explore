// Package web serves the explorer UI. Pages are rendered on the server; the
// only client-side JavaScript is a small live-update listener on the home page.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yu-org/explore/internal/model"
	"github.com/yu-org/explore/internal/store"
)

type Logger interface {
	Printf(format string, v ...any)
}

type Options struct {
	// ChainName is shown in the header and page titles.
	ChainName string
	// NodeURL is displayed in the footer so users know what they are looking at.
	NodeURL string
}

type Server struct {
	store store.Store
	rend  *renderer
	hub   *hub
	mux   *http.ServeMux
	opts  Options
	log   Logger
}

func New(st store.Store, opts Options, log Logger) (*Server, error) {
	rend, err := newRenderer()
	if err != nil {
		return nil, err
	}
	if opts.ChainName == "" {
		opts.ChainName = "Yu"
	}
	s := &Server{store: st, rend: rend, hub: newHub(), mux: http.NewServeMux(), opts: opts, log: log}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /{$}", s.handleHome)
	s.mux.HandleFunc("GET /blocks", s.handleBlocks)
	s.mux.HandleFunc("GET /block/{id}", s.handleBlock)
	s.mux.HandleFunc("GET /txs", s.handleTxs)
	s.mux.HandleFunc("GET /tx/{hash}", s.handleTx)
	s.mux.HandleFunc("GET /address/{address}", s.handleAddress)
	s.mux.HandleFunc("GET /tripods", s.handleTripods)
	s.mux.HandleFunc("GET /search", s.handleSearch)
	s.mux.HandleFunc("GET /stream", s.handleStream)
	s.mux.Handle("GET /static/", http.FileServer(http.FS(staticFS)))
	// Anything unmatched lands here.
	s.mux.HandleFunc("GET /", s.handleNotFound)
}

func (s *Server) Handler() http.Handler { return s.mux }

// --- live updates ---

// liveUpdate is the SSE payload. Rows arrive as rendered HTML so the browser
// only has to insert them: same markup as the server-rendered page, no
// client-side templating and nothing to re-lay out.
type liveUpdate struct {
	BlockRow string    `json:"blockRow"`
	TxRows   []string  `json:"txRows"`
	Stats    liveStats `json:"stats"`
}

type liveStats struct {
	Height       string `json:"height"`
	TotalTxs     string `json:"totalTxs"`
	AvgBlockTime string `json:"avgBlockTime"`
	TPS          string `json:"tps"`
}

// OnBlock is the indexer hook: it renders the new rows once and pushes them to
// every connected browser.
func (s *Server) OnBlock(b *model.Block, txs []*model.Tx) {
	if s.hub.count() == 0 {
		return
	}

	blockRow, err := s.rend.renderPartial("home_block_row", b)
	if err != nil {
		s.log.Printf("web: render block row: %v", err)
		return
	}

	update := liveUpdate{BlockRow: blockRow}
	for _, tx := range txs {
		row, err := s.rend.renderPartial("home_tx_row", tx)
		if err != nil {
			s.log.Printf("web: render tx row: %v", err)
			continue
		}
		update.TxRows = append(update.TxRows, row)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if st, err := s.store.Stats(ctx); err == nil {
		update.Stats = liveStats{
			Height:       comma(st.LatestHeight),
			TotalTxs:     comma(st.TotalTxs),
			AvgBlockTime: fmt.Sprintf("%.1fs", st.AvgBlockTime),
			TPS:          fmt.Sprintf("%.2f", st.TPS),
		}
	}

	payload, err := json.Marshal(update)
	if err != nil {
		s.log.Printf("web: marshal live update: %v", err)
		return
	}
	s.hub.broadcast(payload)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // don't let a proxy buffer the stream

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	// Tell the browser how long to wait before reconnecting.
	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: block\ndata: %s\n\n", msg)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
