// Package yuclient talks to a yu node's kernel HTTP API.
//
// It decodes into yu's own types (types.Block, types.Receipt) rather than
// redeclaring them, so the explorer stays in sync with the framework.
package yuclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yu-org/yu/core/types"
)

// ErrNotFound is returned when the node has no such block or receipt.
var ErrNotFound = fmt.Errorf("not found")

type Client struct {
	base string
	hc   *http.Client
}

func New(endpoint string) *Client {
	return &Client{
		base: strings.TrimSuffix(endpoint, "/"),
		hc:   &http.Client{Timeout: 15 * time.Second},
	}
}

// apiResponse mirrors protocol.APIResponse, with Data left raw so each caller
// decodes it into the type it expects.
type apiResponse struct {
	Code   int             `json:"code"`
	ErrMsg string          `json:"err_msg"`
	Data   json.RawMessage `json:"data"`
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("node returned %s: %s", resp.Status, truncate(body, 200))
	}

	var api apiResponse
	if err := json.Unmarshal(body, &api); err != nil {
		return fmt.Errorf("decode envelope: %w (body: %s)", err, truncate(body, 200))
	}
	if api.Code != 0 {
		// The kernel answers 200 with a non-zero code for missing data.
		if strings.Contains(strings.ToLower(api.ErrMsg), "not found") {
			return ErrNotFound
		}
		return fmt.Errorf("node error %d: %s", api.Code, api.ErrMsg)
	}
	// A successful call with a null payload also means "no such thing".
	if len(api.Data) == 0 || string(api.Data) == "null" {
		return ErrNotFound
	}
	return json.Unmarshal(api.Data, out)
}

// LatestBlock returns the node's current end block.
func (c *Client) LatestBlock(ctx context.Context) (*types.Block, error) {
	block := new(types.Block)
	if err := c.get(ctx, "/api/block", nil, block); err != nil {
		return nil, err
	}
	return block, nil
}

func (c *Client) BlockByHeight(ctx context.Context, height uint64) (*types.Block, error) {
	block := new(types.Block)
	// The kernel parses `number` with hexutil, so it must carry the 0x prefix.
	q := url.Values{"number": []string{"0x" + strconv.FormatUint(height, 16)}}
	if err := c.get(ctx, "/api/block", q, block); err != nil {
		return nil, err
	}
	return block, nil
}

func (c *Client) BlockByHash(ctx context.Context, hash string) (*types.Block, error) {
	block := new(types.Block)
	q := url.Values{"hash": []string{hash}}
	if err := c.get(ctx, "/api/block", q, block); err != nil {
		return nil, err
	}
	return block, nil
}

// ReceiptsByHeight returns the receipts of every transaction in a block.
// A block with no transactions yields an empty slice, not an error.
func (c *Client) ReceiptsByHeight(ctx context.Context, height uint64) ([]*types.Receipt, error) {
	var receipts []*types.Receipt
	// This endpoint parses `block_number` with strconv, so it wants decimal.
	q := url.Values{"block_number": []string{strconv.FormatUint(height, 10)}}
	err := c.get(ctx, "/api/receipts", q, &receipts)
	if err == ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return receipts, nil
}

func (c *Client) Receipt(ctx context.Context, txHash string) (*types.Receipt, error) {
	receipt := new(types.Receipt)
	q := url.Values{"tx_hash": []string{txHash}}
	if err := c.get(ctx, "/api/receipt", q, receipt); err != nil {
		return nil, err
	}
	return receipt, nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
