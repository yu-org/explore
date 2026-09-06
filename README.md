# explore

A block explorer for chains built on the [yu](https://github.com/yu-org/yu) framework.
It indexes a yu node into a local database and serves the UI from a single binary.

## Run

Against an existing chain, point `-node` at its kernel HTTP port:

```sh
go run ./cmd/explore -node http://my-node:7999 -listen :8080
```

Then open <http://localhost:8080>.

Without a chain to point at, `cmd/devnode` starts a single-node PoA chain with the
`asset` tripod and generates transactions to index:

```sh
go run ./cmd/devnode -reset          # terminal 1 — chain on :7999
go run ./cmd/explore                 # terminal 2 — explorer on :8080
```

## Flags

| flag | default | meaning |
| --- | --- | --- |
| `-node` | `http://localhost:7999` | yu node HTTP API endpoint |
| `-listen` | `:8080` | address to serve the UI on |
| `-db` | `explore.db` | path to the index database |
| `-poll` | `1s` | how often to ask the node for its head |
| `-start-height` | `1` | first block to index |

The index is rebuilt from the node, so deleting the database file re-syncs from
scratch.

## Chain identity

The name in the header, the network badge next to it and the version in the
footer are not the explorer's to choose: it asks the node for them over
`GET /api/chain_spec` (yu v1.3.6 and later) and shows what comes back — the
`[chain_spec]` section of the node's `kernel.toml`:

```toml
[chain_spec]
chain_name = "Yu"
author = "yu-org"
version = "v1.0.0"
# mainnet / testnet / devnet
network = "devnet"
```

That is the same identity the node prints in its startup banner. The explorer
keeps asking until the node answers, so it can be started first; against a node
older than v1.3.6 the endpoint is missing and yu's own defaults are shown.

`devnode` takes `-chain-name`, `-network`, `-chain-version` and `-author` if you
want to watch a different identity appear in the UI.

## Search

Accepts a block height, a block hash, a transaction hash or an address.

## License

MIT
