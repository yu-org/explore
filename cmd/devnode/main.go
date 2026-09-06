// Command devnode runs a single-node yu chain for developing the explorer
// against. It is a development tool, not part of the explorer itself.
//
// The node serves the standard yu kernel API on :7999 (http) and :8999 (ws),
// which is exactly what `explore` consumes.
package main

import (
	"flag"
	"math/rand"
	"os"
	"path"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/yu-org/yu/apps/asset"
	"github.com/yu-org/yu/apps/poa"
	"github.com/yu-org/yu/config"
	"github.com/yu-org/yu/core/keypair"
	"github.com/yu-org/yu/core/startup"

	cliasset "github.com/yu-org/yu/example/client/asset"
)

func main() {
	var (
		dataDir  = flag.String("data-dir", "devnet-data", "chain data directory")
		reset    = flag.Bool("reset", false, "wipe the data directory before starting")
		interval = flag.Int("block-interval", 3000, "block interval in milliseconds")
		traffic  = flag.Duration("traffic", 5*time.Second, "interval between generated transactions; 0 disables traffic")

		// The chain's own identity, served over /api/chain_spec and shown by
		// the explorer. Empty means "whatever yu defaults to".
		chainName = flag.String("chain-name", "", "chain_spec.chain_name of this devnet")
		network   = flag.String("network", "", "chain_spec.network: mainnet, testnet or devnet")
		version   = flag.String("chain-version", "", "chain_spec.version of this devnet")
		author    = flag.String("author", "", "chain_spec.author of this devnet")
	)
	flag.Parse()

	if *reset {
		if err := os.RemoveAll(*dataDir); err != nil {
			logrus.Fatal("reset data dir: ", err)
		}
	}

	cfg := config.InitDefaultCfg()
	overrideChainSpec(&cfg.ChainSpec, *chainName, *network, *version, *author)
	cfg.DataDir = *dataDir
	cfg.LogOutput = path.Join(*dataDir, "yu.log")
	cfg.IsAdmin = true
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		logrus.Fatal("create data dir: ", err)
	}

	poaCfg := poa.DefaultCfg(0)
	poaCfg.BlockInterval = *interval

	if *traffic > 0 {
		go generateTraffic(*traffic)
	}

	logrus.Info("devnode http :", cfg.HttpPort, " ws :", cfg.WsPort, " data-dir ", cfg.DataDir)

	startup.InitDefaultKernel(cfg).
		WithTripods(poa.NewPoa(poaCfg), asset.NewAsset("YuCoin")).
		Startup()
}

// overrideChainSpec applies the flags that were actually set, leaving the rest
// of yu's default spec alone.
func overrideChainSpec(spec *config.ChainSpec, name, network, version, author string) {
	for _, f := range []struct {
		value string
		field *string
	}{
		{name, &spec.ChainName},
		{network, &spec.Network},
		{version, &spec.Version},
		{author, &spec.Author},
	} {
		if f.value != "" {
			*f.field = f.value
		}
	}
}

// generateTraffic funds a faucet account and then keeps sending transfers so
// the explorer has something to index. Every failure is logged and ignored —
// this is best-effort dev data.
func generateTraffic(every time.Duration) {
	// Give the kernel time to bind its HTTP port.
	time.Sleep(5 * time.Second)

	faucetPub, faucetPriv, err := keypair.GenKeyPair(keypair.Sr25519)
	if err != nil {
		logrus.Error("traffic: gen faucet key: ", err)
		return
	}
	logrus.Info("traffic: faucet address ", faucetPub.Address().String())

	// A small set of recipients, so address pages have more than one tx.
	const numPeers = 5
	peers := make([]keypair.PubKey, 0, numPeers)
	for i := 0; i < numPeers; i++ {
		pub, _, err := keypair.GenKeyPair(keypair.Sr25519)
		if err != nil {
			logrus.Error("traffic: gen peer key: ", err)
			return
		}
		peers = append(peers, pub)
	}

	safeCall(func() { cliasset.CreateAccount(faucetPriv, faucetPub, 1_000_000_000) })
	time.Sleep(every)

	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for range ticker.C {
		to := peers[rand.Intn(len(peers))]
		amount := uint64(rand.Intn(900) + 100)
		safeCall(func() {
			cliasset.TransferBalance(faucetPriv, faucetPub, to.Address(), amount, 0)
		})
	}
}

// The example client panics on any error; contain it.
func safeCall(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Warn("traffic: ", r)
		}
	}()
	fn()
}
