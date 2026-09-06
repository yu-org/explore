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
		poll        = flag.Duration("poll", time.Second, "how often to poll the node for new blocks")
		startHeight = flag.Uint64("start-height", 1, "first block height to index")
	)
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags)

	if err := run(logger, *nodeURL, *listen, *dbPath, *poll, *startHeight); err != nil {
		logger.Fatal(err)
	}
}

func run(logger *log.Logger, nodeURL, listen, dbPath string, poll time.Duration, startHeight uint64) error {
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

	server, err := web.New(st, web.Options{NodeURL: nodeURL}, logger)
	if err != nil {
		return err
	}
	// The chain names itself; the explorer only reports what it is told.
	go resolveChainSpec(ctx, client, server, poll, logger)

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

// resolveChainSpec asks the node what chain this is and hands the answer to the
// UI. It keeps retrying because the explorer is often started before the node
// is up; until it succeeds the header shows yu's own defaults. A node older
// than v1.3.6 has no such endpoint, so the first failure is logged and the
// rest are not, to avoid filling the log with the same line.
func resolveChainSpec(ctx context.Context, client *yuclient.Client, server *web.Server, retry time.Duration, logger *log.Logger) {
	for logged := false; ; logged = true {
		spec, err := client.ChainSpec(ctx)
		if err == nil {
			server.SetChainSpec(spec)
			logger.Printf("chain: %s %s on %s by %s",
				spec.ChainName, spec.Version, spec.Network, spec.Author)
			return
		}
		if !logged && ctx.Err() == nil {
			logger.Printf("chain spec unavailable, showing defaults until the node answers: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
	}
}
