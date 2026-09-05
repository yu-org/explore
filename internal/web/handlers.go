package web

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/yu-org/explore/internal/model"
	"github.com/yu-org/explore/internal/store"
)

const (
	perPage      = 25
	homeRowLimit = 12
)

// addressPattern is the shape of a yu address: 20 hex bytes.
var addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

// pageData is what every template receives. Page-specific values hang off Data.
type pageData struct {
	Title     string
	Nav       string
	ChainName string
	NodeURL   string
	Query     string
	Data      any
}

func (s *Server) page(title, nav string, data any) pageData {
	return pageData{
		Title:     title,
		Nav:       nav,
		ChainName: s.opts.ChainName,
		NodeURL:   s.opts.NodeURL,
		Data:      data,
	}
}

func (s *Server) fail(w http.ResponseWriter, status int, heading, detail string) {
	s.rend.render(w, status, "error.html", s.page(heading, "", map[string]string{
		"Heading": heading,
		"Detail":  detail,
	}))
}

func (s *Server) internal(w http.ResponseWriter, err error) {
	s.log.Printf("web: %v", err)
	s.fail(w, http.StatusInternalServerError, "Something went wrong",
		"The explorer could not read its index. Check the server logs for details.")
}

func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	s.fail(w, http.StatusNotFound, "Page not found",
		"There is nothing at "+r.URL.Path+".")
}

// pageNum reads ?page=N, 1-based.
func pageNum(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// listing carries pagination state to the templates.
type listing struct {
	Page    int
	PrevURL string
	NextURL string
	HasPrev bool
	HasNext bool
}

func makeListing(path string, page, got, limit int) listing {
	l := listing{Page: page, HasPrev: page > 1, HasNext: got == limit}
	if l.HasPrev {
		l.PrevURL = path + "?page=" + strconv.Itoa(page-1)
	}
	if l.HasNext {
		l.NextURL = path + "?page=" + strconv.Itoa(page+1)
	}
	return l
}

// --- home ---

type homeData struct {
	Stats    *model.ChainStats
	Blocks   []*model.Block
	Txs      []*model.Tx
	RowLimit int
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, err := s.store.Stats(ctx)
	if err != nil {
		s.internal(w, err)
		return
	}
	blocks, err := s.store.Blocks(ctx, store.Page{Limit: homeRowLimit})
	if err != nil {
		s.internal(w, err)
		return
	}
	txs, err := s.store.Txs(ctx, store.Page{Limit: homeRowLimit})
	if err != nil {
		s.internal(w, err)
		return
	}

	s.rend.render(w, http.StatusOK, "home.html",
		s.page("Home", "home", homeData{Stats: stats, Blocks: blocks, Txs: txs, RowLimit: homeRowLimit}))
}

// --- blocks ---

type blocksData struct {
	Blocks []*model.Block
	List   listing
}

func (s *Server) handleBlocks(w http.ResponseWriter, r *http.Request) {
	page := pageNum(r)
	blocks, err := s.store.Blocks(r.Context(), store.Page{
		Limit: perPage, Offset: (page - 1) * perPage,
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	s.rend.render(w, http.StatusOK, "blocks.html", s.page("Blocks", "blocks", blocksData{
		Blocks: blocks,
		List:   makeListing("/blocks", page, len(blocks), perPage),
	}))
}

type blockData struct {
	Block *model.Block
	Txs   []*model.Tx
	Prev  uint64
	Next  uint64
	Head  uint64
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	var (
		block *model.Block
		err   error
	)
	if height, convErr := strconv.ParseUint(id, 10, 64); convErr == nil {
		block, err = s.store.BlockByHeight(ctx, height)
	} else {
		block, err = s.store.BlockByHash(ctx, id)
	}
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "Block not found",
			"No indexed block matches "+id+". It may not have been indexed yet.")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}

	txs, err := s.store.TxsByBlock(ctx, block.Height)
	if err != nil {
		s.internal(w, err)
		return
	}
	stats, err := s.store.Stats(ctx)
	if err != nil {
		s.internal(w, err)
		return
	}

	data := blockData{Block: block, Txs: txs, Head: stats.LatestHeight}
	if block.Height > 1 {
		data.Prev = block.Height - 1
	}
	if block.Height < stats.LatestHeight {
		data.Next = block.Height + 1
	}

	s.rend.render(w, http.StatusOK, "block.html",
		s.page("Block #"+strconv.FormatUint(block.Height, 10), "blocks", data))
}

// --- transactions ---

type txsData struct {
	Txs  []*model.Tx
	List listing
}

func (s *Server) handleTxs(w http.ResponseWriter, r *http.Request) {
	page := pageNum(r)
	txs, err := s.store.Txs(r.Context(), store.Page{
		Limit: perPage, Offset: (page - 1) * perPage,
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	s.rend.render(w, http.StatusOK, "txs.html", s.page("Transactions", "txs", txsData{
		Txs:  txs,
		List: makeListing("/txs", page, len(txs), perPage),
	}))
}

func (s *Server) handleTx(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	tx, err := s.store.TxByHash(r.Context(), hash)
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "Transaction not found",
			"No indexed transaction matches "+hash+".")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	s.rend.render(w, http.StatusOK, "tx.html", s.page("Transaction", "txs", tx))
}

// --- address ---

type addressData struct {
	Address string
	Count   int64
	Txs     []*model.Tx
	List    listing
}

func (s *Server) handleAddress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	address := strings.ToLower(r.PathValue("address"))
	if !addressPattern.MatchString(address) {
		s.fail(w, http.StatusNotFound, "Not an address",
			"An address is 20 hex bytes, like 0x7eae9c835a9368971f313f9f9f0f98d26f056b36.")
		return
	}
	page := pageNum(r)

	txs, err := s.store.TxsByAddress(ctx, address, store.Page{
		Limit: perPage, Offset: (page - 1) * perPage,
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	count, err := s.store.TxCountByAddress(ctx, address)
	if err != nil {
		s.internal(w, err)
		return
	}

	s.rend.render(w, http.StatusOK, "address.html", s.page("Address "+shortHash(address, 10), "", addressData{
		Address: address,
		Count:   count,
		Txs:     txs,
		List:    makeListing("/address/"+address, page, len(txs), perPage),
	}))
}

// --- tripods ---

func (s *Server) handleTripods(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.Tripods(r.Context())
	if err != nil {
		s.internal(w, err)
		return
	}
	s.rend.render(w, http.StatusOK, "tripods.html", s.page("Tripods", "tripods", stats))
}

// --- search ---

// handleSearch resolves a query to the one page it can mean, and redirects.
// Heights, block hashes, transaction hashes and addresses are distinguishable
// by shape, so no disambiguation page is needed.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	lower := strings.ToLower(q)

	// A bare number is a block height.
	if height, err := strconv.ParseUint(q, 10, 64); err == nil {
		if _, err := s.store.BlockByHeight(ctx, height); err == nil {
			http.Redirect(w, r, "/block/"+strconv.FormatUint(height, 10), http.StatusSeeOther)
			return
		}
		s.searchMiss(w, q, "No block at height "+q+" has been indexed.")
		return
	}

	if !strings.HasPrefix(lower, "0x") {
		s.searchMiss(w, q, "Search accepts a block height, a block hash, a transaction hash or an address.")
		return
	}

	switch len(lower) {
	case 66: // 0x + 32 bytes: block hash or tx hash
		if _, err := s.store.BlockByHash(ctx, lower); err == nil {
			http.Redirect(w, r, "/block/"+lower, http.StatusSeeOther)
			return
		}
		if _, err := s.store.TxByHash(ctx, lower); err == nil {
			http.Redirect(w, r, "/tx/"+lower, http.StatusSeeOther)
			return
		}
		s.searchMiss(w, q, "No indexed block or transaction has this hash.")
	case 42: // 0x + 20 bytes: address
		http.Redirect(w, r, "/address/"+lower, http.StatusSeeOther)
	default:
		s.searchMiss(w, q, "That does not look like a hash (32 bytes) or an address (20 bytes).")
	}
}

func (s *Server) searchMiss(w http.ResponseWriter, q, detail string) {
	s.rend.render(w, http.StatusNotFound, "error.html", pageData{
		Title:     "No results",
		ChainName: s.opts.ChainName,
		NodeURL:   s.opts.NodeURL,
		Query:     q,
		Data: map[string]string{
			"Heading": "No results for “" + q + "”",
			"Detail":  detail,
		},
	})
}
