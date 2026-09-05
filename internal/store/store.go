// Package store defines the persistence contract for indexed chain data.
// The SQLite implementation lives in store/sqlitestore; the interface exists so
// a Postgres backend can be dropped in for larger chains without touching the
// indexer or the web layer.
package store

import (
	"context"
	"errors"

	"github.com/yu-org/explore/internal/model"
)

// ErrNotFound is returned by the single-row lookups.
var ErrNotFound = errors.New("not found")

// Page describes a slice of a listing.
type Page struct {
	Limit  int
	Offset int
}

type Store interface {
	// Migrate creates the schema if it is missing.
	Migrate(ctx context.Context) error
	Close() error

	// --- indexer side ---

	// LastIndexedHeight reports the highest stored block. ok is false on an
	// empty database.
	LastIndexedHeight(ctx context.Context) (height uint64, ok bool, err error)
	// BlockHashAt returns the stored hash for a height, for reorg detection.
	BlockHashAt(ctx context.Context, height uint64) (string, error)
	// SaveBlock writes a block and its transactions in one transaction.
	SaveBlock(ctx context.Context, b *model.Block, txs []*model.Tx, refs []model.AddressRef) error
	// DeleteFrom removes every block at or above height, used to unwind a reorg.
	DeleteFrom(ctx context.Context, height uint64) error

	// --- read side ---

	Stats(ctx context.Context) (*model.ChainStats, error)
	Blocks(ctx context.Context, p Page) ([]*model.Block, error)
	BlockByHeight(ctx context.Context, height uint64) (*model.Block, error)
	BlockByHash(ctx context.Context, hash string) (*model.Block, error)

	Txs(ctx context.Context, p Page) ([]*model.Tx, error)
	TxsByBlock(ctx context.Context, height uint64) ([]*model.Tx, error)
	TxByHash(ctx context.Context, hash string) (*model.Tx, error)
	TxsByAddress(ctx context.Context, address string, p Page) ([]*model.Tx, error)
	TxCountByAddress(ctx context.Context, address string) (int64, error)

	// Tripods lists the tripod/writing pairs seen on chain, most used first.
	Tripods(ctx context.Context) ([]model.TripodStat, error)
}
