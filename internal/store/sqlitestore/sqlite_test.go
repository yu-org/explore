package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yu-org/explore/internal/model"
	"github.com/yu-org/explore/internal/store"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func block(height uint64, hash, prev string, txCount int) *model.Block {
	return &model.Block{
		Height: height, Hash: hash, PrevHash: prev,
		Timestamp: int64(1700000000 + height*2), TxCount: txCount,
		LeiUsed: 10, LeiLimit: 50000,
	}
}

func tx(hash string, height uint64, caller string) *model.Tx {
	return &model.Tx{
		Hash: hash, Height: height, Caller: caller, Tripod: "asset", Writing: "Transfer",
		Params: `{"amount":1}`, Success: true, Events: []string{"done"},
	}
}

func TestSaveAndReadBack(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	b := block(1, "0xb1", "0x00", 1)
	tr := tx("0xt1", 1, "0xcaller")
	refs := []model.AddressRef{{Address: "0xcaller", TxHash: "0xt1", Height: 1, Role: model.RoleCaller}}

	if err := st.SaveBlock(ctx, b, []*model.Tx{tr}, refs); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.BlockByHeight(ctx, 1)
	if err != nil {
		t.Fatalf("block by height: %v", err)
	}
	if got.Hash != "0xb1" || got.TxCount != 1 {
		t.Errorf("block = %+v", got)
	}

	byHash, err := st.BlockByHash(ctx, "0xb1")
	if err != nil || byHash.Height != 1 {
		t.Errorf("block by hash = %+v, err %v", byHash, err)
	}

	gotTx, err := st.TxByHash(ctx, "0xt1")
	if err != nil {
		t.Fatalf("tx by hash: %v", err)
	}
	if gotTx.Writing != "Transfer" || len(gotTx.Events) != 1 || gotTx.Events[0] != "done" {
		t.Errorf("tx = %+v", gotTx)
	}

	txs, err := st.TxsByAddress(ctx, "0xCALLER", store.Page{Limit: 10})
	if err != nil {
		t.Fatalf("txs by address: %v", err)
	}
	if len(txs) != 1 {
		t.Errorf("got %d txs for address, want 1", len(txs))
	}

	n, err := st.TxCountByAddress(ctx, "0xcaller")
	if err != nil || n != 1 {
		t.Errorf("count = %d, err %v", n, err)
	}
}

// Re-indexing a height must overwrite rather than fail, so a restarted or
// rewound indexer can replay blocks safely.
func TestSaveBlockIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	b := block(1, "0xb1", "0x00", 1)
	tr := tx("0xt1", 1, "0xcaller")
	refs := []model.AddressRef{{Address: "0xcaller", TxHash: "0xt1", Height: 1, Role: model.RoleCaller}}

	for i := 0; i < 2; i++ {
		if err := st.SaveBlock(ctx, b, []*model.Tx{tr}, refs); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.IndexedBlocks != 1 || stats.TotalTxs != 1 {
		t.Errorf("stats = %+v, want 1 block and 1 tx", stats)
	}
}

func TestDeleteFromUnwindsEverything(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	for h := uint64(1); h <= 3; h++ {
		b := block(h, hashFor(h), hashFor(h-1), 1)
		tr := tx("0xt"+string(rune('0'+h)), h, "0xcaller")
		refs := []model.AddressRef{{Address: "0xcaller", TxHash: tr.Hash, Height: h, Role: model.RoleCaller}}
		if err := st.SaveBlock(ctx, b, []*model.Tx{tr}, refs); err != nil {
			t.Fatalf("save %d: %v", h, err)
		}
	}

	if err := st.DeleteFrom(ctx, 2); err != nil {
		t.Fatalf("delete: %v", err)
	}

	last, ok, err := st.LastIndexedHeight(ctx)
	if err != nil || !ok || last != 1 {
		t.Errorf("last height = %d (ok %v), want 1", last, ok)
	}
	if _, err := st.BlockByHeight(ctx, 2); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("block 2 err = %v, want ErrNotFound", err)
	}
	n, err := st.TxCountByAddress(ctx, "0xcaller")
	if err != nil || n != 1 {
		t.Errorf("address refs after unwind = %d, want 1", n)
	}
}

func TestEmptyStore(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	if _, ok, err := st.LastIndexedHeight(ctx); err != nil || ok {
		t.Errorf("empty store reported a height (ok %v, err %v)", ok, err)
	}
	stats, err := st.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.IndexedBlocks != 0 || stats.AvgBlockTime != 0 {
		t.Errorf("stats = %+v, want zeroed", stats)
	}
	if _, err := st.BlockByHeight(ctx, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := st.TxByHash(ctx, "0xnope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func hashFor(h uint64) string {
	return "0xb" + string(rune('0'+h))
}
