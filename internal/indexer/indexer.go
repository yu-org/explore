// Package indexer walks a yu chain and keeps the explorer's store in sync.
//
// The kernel API only answers "give me block N" and "give me the receipts of
// block N", so everything an explorer needs beyond that — listings, address
// history, per-writing stats — is built here.
package indexer

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/yu-org/explore/internal/model"
	"github.com/yu-org/explore/internal/store"
	"github.com/yu-org/explore/internal/yuclient"
	yutypes "github.com/yu-org/yu/core/types"
)

// Logger is the minimal logging surface the indexer needs.
type Logger interface {
	Printf(format string, v ...any)
}

// OnBlock is called after a block has been committed to the store, so the web
// layer can push it to connected browsers.
type OnBlock func(*model.Block, []*model.Tx)

type Config struct {
	// PollInterval is how often to ask the node for its head when the indexer
	// has caught up.
	PollInterval time.Duration
	// StartHeight is the first block to index. yu chains begin at height 1.
	StartHeight uint64
}

func (c *Config) setDefaults() {
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.StartHeight == 0 {
		c.StartHeight = 1
	}
}

type Indexer struct {
	client *yuclient.Client
	store  store.Store
	cfg    Config
	log    Logger

	onBlock OnBlock
}

func New(client *yuclient.Client, st store.Store, cfg Config, log Logger) *Indexer {
	cfg.setDefaults()
	return &Indexer{client: client, store: st, cfg: cfg, log: log}
}

// SetOnBlock registers the post-commit hook. Not safe to call once Run started.
func (ix *Indexer) SetOnBlock(fn OnBlock) { ix.onBlock = fn }

// Run indexes until ctx is cancelled. Transient node errors are logged and
// retried rather than fatal: a restarting node should not take the UI down.
func (ix *Indexer) Run(ctx context.Context) error {
	ticker := time.NewTicker(ix.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if err := ix.catchUp(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			ix.log.Printf("indexer: %v", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// catchUp indexes everything between the stored tip and the node's head.
func (ix *Indexer) catchUp(ctx context.Context) error {
	head, err := ix.client.LatestBlock(ctx)
	if err != nil {
		return err
	}
	headHeight := uint64(head.Height)

	next, err := ix.nextHeight(ctx)
	if err != nil {
		return err
	}

	for h := next; h <= headHeight; h++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rewound, err := ix.indexHeight(ctx, h)
		if err != nil {
			return err
		}
		if rewound {
			// The chain forked under us; restart the range from the store's
			// new tip on the next pass.
			return nil
		}
	}
	return nil
}

func (ix *Indexer) nextHeight(ctx context.Context) (uint64, error) {
	last, ok, err := ix.store.LastIndexedHeight(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		return ix.cfg.StartHeight, nil
	}
	return last + 1, nil
}

// indexHeight fetches, converts and stores one block. It reports rewound=true
// when it detected a fork and unwound the store instead of writing.
func (ix *Indexer) indexHeight(ctx context.Context, height uint64) (rewound bool, err error) {
	block, err := ix.client.BlockByHeight(ctx, height)
	if err != nil {
		if errors.Is(err, yuclient.ErrNotFound) {
			// The head moved between our query and this fetch; try later.
			return false, nil
		}
		return false, err
	}

	mb := model.BlockFromChain(block)

	// Reorg check: our stored parent must be this block's parent.
	if height > ix.cfg.StartHeight {
		storedParent, err := ix.store.BlockHashAt(ctx, height-1)
		switch {
		case errors.Is(err, store.ErrNotFound):
			// Nothing stored yet at the parent height; nothing to compare.
		case err != nil:
			return false, err
		case !strings.EqualFold(storedParent, mb.PrevHash):
			ix.log.Printf("indexer: fork detected at height %d (stored parent %s, block parent %s)",
				height, short(storedParent), short(mb.PrevHash))
			// Drop the parent and retry. If its own parent disagrees too, the
			// next pass unwinds one more block, walking back to the common
			// ancestor without guessing how deep the fork goes.
			if err := ix.store.DeleteFrom(ctx, height-1); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	receipts, err := ix.client.ReceiptsByHeight(ctx, height)
	if err != nil {
		// A block whose receipts are not readable is still worth indexing;
		// the transactions just show up without execution results.
		ix.log.Printf("indexer: receipts for height %d: %v", height, err)
	}
	byHash := make(map[string]*yutypes.Receipt, len(receipts))
	for _, r := range receipts {
		if r != nil {
			byHash[strings.ToLower(r.TxHash.String())] = r
		}
	}

	txs := make([]*model.Tx, 0, len(block.Txns))
	refs := make([]model.AddressRef, 0, len(block.Txns))
	for i, st := range block.Txns {
		if st == nil || st.Raw == nil {
			continue
		}
		tx := model.TxFromChain(mb, i, st, byHash[strings.ToLower(st.TxnHash.String())])
		txs = append(txs, tx)
		refs = append(refs, addressRefs(tx)...)
	}

	if err := ix.store.SaveBlock(ctx, mb, txs, refs); err != nil {
		return false, err
	}
	if ix.onBlock != nil {
		ix.onBlock(mb, txs)
	}
	return false, nil
}

// addrPattern matches a 20-byte hex address as it appears in call params and
// event payloads.
var addrPattern = regexp.MustCompile(`0x[0-9a-fA-F]{40}`)

// addressRefs derives every address this transaction touches. The caller comes
// from the receipt; the rest are scraped from params and events, which is the
// only generic way to find them — a tripod's params are opaque JSON to us.
func addressRefs(tx *model.Tx) []model.AddressRef {
	seen := make(map[string]string) // address -> role
	add := func(addr, role string) {
		addr = strings.ToLower(addr)
		if addr == "" || addr == emptyAddress {
			return
		}
		if existing, ok := seen[addr]; ok {
			// caller is the strongest role; never downgrade it.
			if existing == model.RoleCaller {
				return
			}
		}
		seen[addr] = role
	}

	add(tx.Caller, model.RoleCaller)
	for _, m := range addrPattern.FindAllString(tx.Params, -1) {
		add(m, model.RoleParam)
	}
	for _, ev := range tx.Events {
		for _, m := range addrPattern.FindAllString(ev, -1) {
			add(m, model.RoleEvent)
		}
	}

	refs := make([]model.AddressRef, 0, len(seen))
	for addr, role := range seen {
		refs = append(refs, model.AddressRef{
			Address: addr, TxHash: tx.Hash, Height: tx.Height, Role: role,
		})
	}
	return refs
}

const emptyAddress = "0x0000000000000000000000000000000000000000"

func short(hash string) string {
	if len(hash) <= 12 {
		return hash
	}
	return hash[:10] + "…"
}
