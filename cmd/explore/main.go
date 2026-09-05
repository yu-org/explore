// Command explore is a block explorer for chains built on the yu framework.
//
// It indexes a node's kernel API into a local database and serves a
// server-rendered UI over it. One binary, no external services:
//
//	explore -node http://localhost:7999 -listen :8080
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yu-org/explore/internal/indexer"
	"github.com/yu-org/explore/internal/store/sqlitestore"
	"github.com/yu-org/explore/internal/web"
	"github.com/yu-org/explore/internal/yuclient"
)

func main() {
	var (
		nodeURL     = flag.String("node", "http://localhost:7999", "yu node HTTP API endpoint")
		listen      = flag.String("listen", ":8080", "address to serve the explorer on")
		dbPath      = flag.String("db", "explore.db", "path to the index database")
		chainName   = flag.String("chain-name", "Yu", "chain name shown in the UI")
		poll        = flag.Duration("poll", time.Second, "how often to poll the node for new blocks")
		startHeight = flag.Uint64("start-height", 1, "first block height to index")
	)
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags)

	if err := run(logger, *nodeURL, *listen, *dbPath, *chainName, *poll, *startHeight); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger, nodeURL, listen, dbPath, chainName string, poll time.Duration, startHeight uint64) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := sqlitestore.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}

	client := yuclient.New(nodeURL)

	server, err := web.New(st, web.Options{ChainName: chainName, NodeURL: nodeURL}, logger)
	if err != nil {
		return err
	}

	ix := indexer.New(client, st, indexer.Config{
		PollInterval: poll,
		StartHeight:  startHeight,
	}, logger)
	ix.SetOnBlock(server.OnBlock)

	go func() {
		if err := ix.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("indexer stopped: %v", err)
		}
	}()

	httpServer := &http.Server{
		Addr:              listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: the /stream endpoint is a long-lived response.
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Printf("indexing %s into %s", nodeURL, dbPath)
	logger.Printf("explorer listening on http://localhost%s", listen)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
