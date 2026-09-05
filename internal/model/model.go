// Package model holds the explorer's own view of chain data: what gets
// indexed, stored and rendered. It is deliberately flatter than yu's on-chain
// types — a receipt and its transaction are one row here, because that is how
// a person reads them.
package model

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/yu-org/yu/core/types"
)

// Block is one indexed block header plus a few derived counters.
type Block struct {
	Height      uint64
	Hash        string
	PrevHash    string
	Timestamp   int64
	TxCount     int
	LeiUsed     uint64
	LeiLimit    uint64
	TxnRoot     string
	StateRoot   string
	ReceiptRoot string
	PeerID      string
	MinerPubkey string
	ChainID     uint64
	Difficulty  uint64
	Nonce       uint64
}

// Tx is a signed transaction joined with its execution receipt.
type Tx struct {
	Hash      string
	Height    uint64
	BlockHash string
	Index     int
	Timestamp int64

	Caller  string
	Tripod  string
	Writing string
	Params  string

	LeiPrice uint64
	Tips     uint64
	LeiCost  uint64

	Success bool
	Error   string

	Pubkey    string
	Signature string

	Events []string
}

// Status renders as the badge text used across the UI.
func (t *Tx) Status() string {
	if t.Success {
		return "Success"
	}
	return "Failed"
}

// Address roles recorded in the address index.
const (
	RoleCaller = "caller"
	RoleParam  = "param"
	RoleEvent  = "event"
)

// AddressRef links an address to a transaction it appears in.
type AddressRef struct {
	Address string
	TxHash  string
	Height  uint64
	Role    string
}

// ChainStats is the summary strip on the home page.
type ChainStats struct {
	ChainID       uint64
	LatestHeight  uint64
	IndexedBlocks int64
	TotalTxs      int64
	AvgBlockTime  float64 // seconds, over a recent window
	TPS           float64 // over the same window
	LastBlockTime int64
}

// BlockFromChain converts a yu block header into the indexed form.
func BlockFromChain(b *types.Block) *Block {
	return &Block{
		Height:      uint64(b.Height),
		Hash:        b.Hash.String(),
		PrevHash:    b.PrevHash.String(),
		Timestamp:   int64(b.Timestamp),
		TxCount:     len(b.Txns),
		LeiUsed:     b.LeiUsed,
		LeiLimit:    b.LeiLimit,
		TxnRoot:     b.TxnRoot.String(),
		StateRoot:   b.StateRoot.String(),
		ReceiptRoot: b.ReceiptRoot.String(),
		PeerID:      b.PeerID.String(),
		MinerPubkey: hexOrEmpty(b.MinerPubkey),
		ChainID:     b.ChainID,
		Difficulty:  b.Difficulty,
		Nonce:       b.Nonce,
	}
}

// TxFromChain merges a signed transaction with its receipt. The receipt may be
// nil when the node has not stored one (a tx included but not yet executed).
func TxFromChain(block *Block, idx int, st *types.SignedTxn, rc *types.Receipt) *Tx {
	tx := &Tx{
		Hash:      st.TxnHash.String(),
		Height:    block.Height,
		BlockHash: block.Hash,
		Index:     idx,
		Timestamp: block.Timestamp,
		Pubkey:    hexOrEmpty(st.Pubkey),
		Signature: hexOrEmpty(st.Signature),
		Success:   true,
	}
	if wr := st.Raw.WrCall; wr != nil {
		tx.Tripod = wr.TripodName
		tx.Writing = wr.FuncName
		tx.Params = wr.Params
		tx.LeiPrice = wr.LeiPrice
		tx.Tips = wr.Tips
	}
	// The signed tx carries the caller as raw bytes and may leave it unset;
	// the receipt always has it. Prefer the receipt, fall back to the tx.
	if len(st.Address) > 0 {
		tx.Caller = "0x" + hex.EncodeToString(st.Address)
	}
	if rc != nil {
		if rc.Caller != nil {
			tx.Caller = strings.ToLower(rc.Caller.String())
		}
		if rc.TripodName != "" {
			tx.Tripod = rc.TripodName
		}
		if rc.WritingName != "" {
			tx.Writing = rc.WritingName
		}
		tx.LeiCost = rc.LeiCost
		tx.Error = rc.Error
		tx.Success = rc.Error == ""
		for _, ev := range rc.Events {
			tx.Events = append(tx.Events, DecodeEventValue(ev.Value))
		}
	}
	tx.Caller = strings.ToLower(tx.Caller)
	return tx
}

// DecodeEventValue renders an event payload for humans: tripods emit plain
// strings most of the time, JSON sometimes, and arbitrary bytes occasionally.
func DecodeEventValue(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	if utf8.Valid(v) && !hasControlBytes(v) {
		return string(v)
	}
	return "0x" + hex.EncodeToString(v)
}

// EventsJSON serialises events for storage in a single column.
func EventsJSON(events []string) string {
	if len(events) == 0 {
		return ""
	}
	b, err := json.Marshal(events)
	if err != nil {
		return ""
	}
	return string(b)
}

// EventsFromJSON is the inverse of EventsJSON.
func EventsFromJSON(s string) []string {
	if s == "" {
		return nil
	}
	var events []string
	if err := json.Unmarshal([]byte(s), &events); err != nil {
		return nil
	}
	return events
}

func hasControlBytes(v []byte) bool {
	for _, b := range v {
		if b < 0x20 && b != '\n' && b != '\t' && b != '\r' {
			return true
		}
	}
	return false
}

func hexOrEmpty(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return "0x" + hex.EncodeToString(b)
}

// TripodStat counts how often a writing has been called.
type TripodStat struct {
	Tripod  string
	Writing string
	Count   int64
}
