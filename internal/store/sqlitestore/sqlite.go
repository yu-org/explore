// Package sqlitestore implements store.Store on SQLite via a pure-Go driver,
// so the explorer stays a single static binary with no CGO.
package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/yu-org/explore/internal/model"
	"github.com/yu-org/explore/internal/store"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	// WAL keeps the indexer's writes from blocking page reads.
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// WAL lets page renders read while the indexer writes. Writes still
	// serialise inside SQLite, and busy_timeout covers the contention.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS blocks (
    height       INTEGER PRIMARY KEY,
    hash         TEXT    NOT NULL UNIQUE,
    prev_hash    TEXT    NOT NULL DEFAULT '',
    timestamp    INTEGER NOT NULL DEFAULT 0,
    tx_count     INTEGER NOT NULL DEFAULT 0,
    lei_used     INTEGER NOT NULL DEFAULT 0,
    lei_limit    INTEGER NOT NULL DEFAULT 0,
    txn_root     TEXT    NOT NULL DEFAULT '',
    state_root   TEXT    NOT NULL DEFAULT '',
    receipt_root TEXT    NOT NULL DEFAULT '',
    peer_id      TEXT    NOT NULL DEFAULT '',
    miner_pubkey TEXT    NOT NULL DEFAULT '',
    chain_id     INTEGER NOT NULL DEFAULT 0,
    difficulty   INTEGER NOT NULL DEFAULT 0,
    nonce        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS txs (
    hash       TEXT    PRIMARY KEY,
    height     INTEGER NOT NULL,
    block_hash TEXT    NOT NULL DEFAULT '',
    idx        INTEGER NOT NULL DEFAULT 0,
    timestamp  INTEGER NOT NULL DEFAULT 0,
    caller     TEXT    NOT NULL DEFAULT '',
    tripod     TEXT    NOT NULL DEFAULT '',
    writing    TEXT    NOT NULL DEFAULT '',
    params     TEXT    NOT NULL DEFAULT '',
    lei_price  INTEGER NOT NULL DEFAULT 0,
    tips       INTEGER NOT NULL DEFAULT 0,
    lei_cost   INTEGER NOT NULL DEFAULT 0,
    success    INTEGER NOT NULL DEFAULT 1,
    error      TEXT    NOT NULL DEFAULT '',
    pubkey     TEXT    NOT NULL DEFAULT '',
    signature  TEXT    NOT NULL DEFAULT '',
    events     TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_txs_height ON txs (height DESC, idx ASC);
CREATE INDEX IF NOT EXISTS idx_txs_tripod ON txs (tripod, writing);

-- An address appears in a transaction as its caller, inside the call params,
-- or inside an emitted event. Indexing all three gives useful address pages
-- for arbitrary tripods without the explorer knowing their semantics.
CREATE TABLE IF NOT EXISTS tx_addresses (
    address TEXT    NOT NULL,
    tx_hash TEXT    NOT NULL,
    height  INTEGER NOT NULL,
    role    TEXT    NOT NULL,
    PRIMARY KEY (address, tx_hash, role)
);
CREATE INDEX IF NOT EXISTS idx_tx_addresses_addr ON tx_addresses (address, height DESC);
`

func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) LastIndexedHeight(ctx context.Context) (uint64, bool, error) {
	var height sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(height) FROM blocks`).Scan(&height)
	if err != nil {
		return 0, false, err
	}
	if !height.Valid {
		return 0, false, nil
	}
	return uint64(height.Int64), true, nil
}

func (s *Store) BlockHashAt(ctx context.Context, height uint64) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT hash FROM blocks WHERE height = ?`, height).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", store.ErrNotFound
	}
	return hash, err
}

func (s *Store) SaveBlock(ctx context.Context, b *model.Block, txs []*model.Tx, refs []model.AddressRef) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
        INSERT INTO blocks (height, hash, prev_hash, timestamp, tx_count, lei_used, lei_limit,
                            txn_root, state_root, receipt_root, peer_id, miner_pubkey,
                            chain_id, difficulty, nonce)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(height) DO UPDATE SET
            hash=excluded.hash, prev_hash=excluded.prev_hash, timestamp=excluded.timestamp,
            tx_count=excluded.tx_count, lei_used=excluded.lei_used, lei_limit=excluded.lei_limit,
            txn_root=excluded.txn_root, state_root=excluded.state_root,
            receipt_root=excluded.receipt_root, peer_id=excluded.peer_id,
            miner_pubkey=excluded.miner_pubkey, chain_id=excluded.chain_id,
            difficulty=excluded.difficulty, nonce=excluded.nonce`,
		b.Height, b.Hash, b.PrevHash, b.Timestamp, b.TxCount, b.LeiUsed, b.LeiLimit,
		b.TxnRoot, b.StateRoot, b.ReceiptRoot, b.PeerID, b.MinerPubkey,
		b.ChainID, b.Difficulty, b.Nonce)
	if err != nil {
		return fmt.Errorf("insert block %d: %w", b.Height, err)
	}

	for _, t := range txs {
		_, err = tx.ExecContext(ctx, `
            INSERT INTO txs (hash, height, block_hash, idx, timestamp, caller, tripod, writing,
                             params, lei_price, tips, lei_cost, success, error, pubkey, signature, events)
            VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
            ON CONFLICT(hash) DO UPDATE SET
                height=excluded.height, block_hash=excluded.block_hash, idx=excluded.idx,
                timestamp=excluded.timestamp, caller=excluded.caller, tripod=excluded.tripod,
                writing=excluded.writing, params=excluded.params, lei_price=excluded.lei_price,
                tips=excluded.tips, lei_cost=excluded.lei_cost, success=excluded.success,
                error=excluded.error, pubkey=excluded.pubkey, signature=excluded.signature,
                events=excluded.events`,
			t.Hash, t.Height, t.BlockHash, t.Index, t.Timestamp, t.Caller, t.Tripod, t.Writing,
			t.Params, t.LeiPrice, t.Tips, t.LeiCost, boolToInt(t.Success), t.Error,
			t.Pubkey, t.Signature, model.EventsJSON(t.Events))
		if err != nil {
			return fmt.Errorf("insert tx %s: %w", t.Hash, err)
		}
	}

	for _, r := range refs {
		_, err = tx.ExecContext(ctx, `
            INSERT INTO tx_addresses (address, tx_hash, height, role) VALUES (?,?,?,?)
            ON CONFLICT(address, tx_hash, role) DO NOTHING`,
			r.Address, r.TxHash, r.Height, r.Role)
		if err != nil {
			return fmt.Errorf("insert address ref: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) DeleteFrom(ctx context.Context, height uint64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`DELETE FROM tx_addresses WHERE height >= ?`,
		`DELETE FROM txs WHERE height >= ?`,
		`DELETE FROM blocks WHERE height >= ?`,
	} {
		if _, err := tx.ExecContext(ctx, stmt, height); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Stats(ctx context.Context) (*model.ChainStats, error) {
	st := new(model.ChainStats)

	err := s.db.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(height), 0), COUNT(*), COALESCE(MAX(chain_id), 0)
        FROM blocks`).Scan(&st.LatestHeight, &st.IndexedBlocks, &st.ChainID)
	if err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM txs`).Scan(&st.TotalTxs); err != nil {
		return nil, err
	}
	if st.IndexedBlocks == 0 {
		return st, nil
	}

	// Average block time and TPS over the most recent window of blocks.
	const window = 100
	var (
		first, last sql.NullInt64
		n           int64
		txsInWindow sql.NullInt64
	)
	err = s.db.QueryRowContext(ctx, `
        SELECT MIN(timestamp), MAX(timestamp), COUNT(*), COALESCE(SUM(tx_count), 0)
        FROM (SELECT timestamp, tx_count FROM blocks ORDER BY height DESC LIMIT ?)`,
		window).Scan(&first, &last, &n, &txsInWindow)
	if err != nil {
		return nil, err
	}
	st.LastBlockTime = last.Int64
	if n > 1 && last.Int64 > first.Int64 {
		span := float64(last.Int64 - first.Int64)
		st.AvgBlockTime = span / float64(n-1)
		st.TPS = float64(txsInWindow.Int64) / span
	}
	return st, nil
}

const blockCols = `height, hash, prev_hash, timestamp, tx_count, lei_used, lei_limit,
                   txn_root, state_root, receipt_root, peer_id, miner_pubkey,
                   chain_id, difficulty, nonce`

func scanBlock(row interface{ Scan(...any) error }) (*model.Block, error) {
	b := new(model.Block)
	err := row.Scan(&b.Height, &b.Hash, &b.PrevHash, &b.Timestamp, &b.TxCount, &b.LeiUsed,
		&b.LeiLimit, &b.TxnRoot, &b.StateRoot, &b.ReceiptRoot, &b.PeerID, &b.MinerPubkey,
		&b.ChainID, &b.Difficulty, &b.Nonce)
	return b, err
}

func (s *Store) Blocks(ctx context.Context, p store.Page) ([]*model.Block, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+blockCols+` FROM blocks ORDER BY height DESC LIMIT ? OFFSET ?`,
		p.Limit, p.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blocks []*model.Block
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, b)
	}
	return blocks, rows.Err()
}

func (s *Store) BlockByHeight(ctx context.Context, height uint64) (*model.Block, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+blockCols+` FROM blocks WHERE height = ?`, height)
	b, err := scanBlock(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return b, err
}

func (s *Store) BlockByHash(ctx context.Context, hash string) (*model.Block, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+blockCols+` FROM blocks WHERE hash = ?`, strings.ToLower(hash))
	b, err := scanBlock(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return b, err
}

const txCols = `hash, height, block_hash, idx, timestamp, caller, tripod, writing, params,
                lei_price, tips, lei_cost, success, error, pubkey, signature, events`

func scanTx(row interface{ Scan(...any) error }) (*model.Tx, error) {
	t := new(model.Tx)
	var (
		success int
		events  string
	)
	err := row.Scan(&t.Hash, &t.Height, &t.BlockHash, &t.Index, &t.Timestamp, &t.Caller,
		&t.Tripod, &t.Writing, &t.Params, &t.LeiPrice, &t.Tips, &t.LeiCost, &success,
		&t.Error, &t.Pubkey, &t.Signature, &events)
	if err != nil {
		return nil, err
	}
	t.Success = success == 1
	t.Events = model.EventsFromJSON(events)
	return t, nil
}

func (s *Store) queryTxs(ctx context.Context, query string, args ...any) ([]*model.Tx, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []*model.Tx
	for rows.Next() {
		t, err := scanTx(rows)
		if err != nil {
			return nil, err
		}
		txs = append(txs, t)
	}
	return txs, rows.Err()
}

func (s *Store) Txs(ctx context.Context, p store.Page) ([]*model.Tx, error) {
	return s.queryTxs(ctx,
		`SELECT `+txCols+` FROM txs ORDER BY height DESC, idx ASC LIMIT ? OFFSET ?`,
		p.Limit, p.Offset)
}

func (s *Store) TxsByBlock(ctx context.Context, height uint64) ([]*model.Tx, error) {
	return s.queryTxs(ctx,
		`SELECT `+txCols+` FROM txs WHERE height = ? ORDER BY idx ASC`, height)
}

func (s *Store) TxByHash(ctx context.Context, hash string) (*model.Tx, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+txCols+` FROM txs WHERE hash = ?`, strings.ToLower(hash))
	t, err := scanTx(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return t, err
}

func (s *Store) TxsByAddress(ctx context.Context, address string, p store.Page) ([]*model.Tx, error) {
	return s.queryTxs(ctx, `
        SELECT `+prefixCols("t.", txCols)+`
        FROM txs t
        JOIN (SELECT DISTINCT tx_hash, height FROM tx_addresses WHERE address = ?) a
          ON a.tx_hash = t.hash
        ORDER BY t.height DESC, t.idx ASC
        LIMIT ? OFFSET ?`, strings.ToLower(address), p.Limit, p.Offset)
}

func (s *Store) TxCountByAddress(ctx context.Context, address string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT tx_hash) FROM tx_addresses WHERE address = ?`,
		strings.ToLower(address)).Scan(&n)
	return n, err
}

func (s *Store) Tripods(ctx context.Context) ([]model.TripodStat, error) {
	rows, err := s.db.QueryContext(ctx, `
        SELECT tripod, writing, COUNT(*) AS n FROM txs
        GROUP BY tripod, writing ORDER BY n DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []model.TripodStat
	for rows.Next() {
		var st model.TripodStat
		if err := rows.Scan(&st.Tripod, &st.Writing, &st.Count); err != nil {
			return nil, err
		}
		stats = append(stats, st)
	}
	return stats, rows.Err()
}

// prefixCols qualifies a column list with a table alias.
func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

var _ store.Store = (*Store)(nil)
