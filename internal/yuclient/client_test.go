package yuclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yu-org/yu/config"
	"github.com/yu-org/yu/core/protocol"
)

func TestChainSpec(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != protocol.ChainSpecPath {
			t.Errorf("path = %q, want %q", r.URL.Path, protocol.ChainSpecPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"chain_name":"Liberty","author":"someone",` +
			`"version":"v2.1.0","network":"testnet"}}`))
	}))
	defer srv.Close()

	spec, err := New(srv.URL).ChainSpec(context.Background())
	if err != nil {
		t.Fatalf("ChainSpec: %v", err)
	}
	want := config.ChainSpec{
		ChainName: "Liberty", Author: "someone", Version: "v2.1.0", Network: config.Testnet,
	}
	if spec != want {
		t.Errorf("spec = %+v, want %+v", spec, want)
	}
}

// A node older than yu v1.3.6 has no such route, and the explorer must survive
// the 404 rather than treat it as fatal.
func TestChainSpecOnOldNode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).ChainSpec(context.Background()); err == nil {
		t.Fatal("ChainSpec on a node without the endpoint: want error, got nil")
	}
}
