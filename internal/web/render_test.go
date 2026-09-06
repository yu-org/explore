package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yu-org/yu/config"

	"github.com/yu-org/explore/internal/model"
)

// Templates are parsed at startup, so a typo in one is a startup failure rather
// than a compile error. This test is what catches it.
func TestTemplatesParse(t *testing.T) {
	if _, err := newRenderer(); err != nil {
		t.Fatalf("newRenderer: %v", err)
	}
}

func TestRenderHomeWithNoData(t *testing.T) {
	rend, err := newRenderer()
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	rec := httptest.NewRecorder()
	rend.render(rec, 200, "home.html", pageData{
		Title:     "Home",
		Nav:       "home",
		ChainName: "Yu",
		Data:      homeData{Stats: &model.ChainStats{}},
	})

	if rec.Code != 200 {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Latest Blocks", "Latest Transactions", `id="live-blocks"`, "empty-row"} {
		if !strings.Contains(body, want) {
			t.Errorf("home page missing %q", want)
		}
	}
}

func TestRenderPartialsMatchPageMarkup(t *testing.T) {
	rend, err := newRenderer()
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	row, err := rend.renderPartial("home_block_row", &model.Block{Height: 42, Hash: "0xabc", TxCount: 2})
	if err != nil {
		t.Fatalf("render block_row: %v", err)
	}
	if !strings.Contains(row, `href="/block/42"`) {
		t.Errorf("block row = %s", row)
	}

	txRow, err := rend.renderPartial("home_tx_row", &model.Tx{
		Hash: "0xdef", Height: 42, Tripod: "asset", Writing: "Transfer", Success: true,
	})
	if err != nil {
		t.Fatalf("render tx_row: %v", err)
	}
	for _, want := range []string{`href="/tx/0xdef"`, "badge-ok", "Transfer"} {
		if !strings.Contains(txRow, want) {
			t.Errorf("tx row missing %q: %s", want, txRow)
		}
	}
}

// The header and footer must show what the node calls itself, never a name the
// explorer picked. This renders a spec that shares nothing with yu's defaults, so
// a hardcoded fallback creeping back in would fail here.
func TestRenderShowsChainSpec(t *testing.T) {
	rend, err := newRenderer()
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	rec := httptest.NewRecorder()
	rend.render(rec, 200, "home.html", pageData{
		Title:     "Home",
		Nav:       "home",
		ChainName: "Liberty",
		Network:   config.Testnet,
		Version:   "v2.1.0",
		Author:    "someone",
		Data:      homeData{Stats: &model.ChainStats{}},
	})

	body := rec.Body.String()
	for _, want := range []string{
		"<title>Home | Liberty Explorer</title>",
		`class="net net-testnet"`,
		">testnet<",
		">v2.1.0<",
		"by someone",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
	if strings.Contains(body, "Yu Explorer") {
		t.Error("page shows a chain name the node never reported")
	}
}

func TestNetClass(t *testing.T) {
	tests := map[string]string{
		"mainnet": "mainnet", "testnet": "testnet", "devnet": "devnet",
		"  DevNet ": "devnet", "staging": "other", "": "other",
	}
	for in, want := range tests {
		if got := netClass(in); got != want {
			t.Errorf("netClass(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestComma(t *testing.T) {
	tests := map[any]string{
		0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567",
		int64(-4321): "-4,321", uint64(1000000): "1,000,000",
	}
	for in, want := range tests {
		if got := comma(in); got != want {
			t.Errorf("comma(%v) = %q, want %q", in, got, want)
		}
	}
}
