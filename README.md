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
| `-chain-name` | `Yu` | name shown in the header and page titles |
| `-poll` | `1s` | how often to ask the node for its head |
| `-start-height` | `1` | first block to index |

The index is rebuilt from the node, so deleting the database file re-syncs from
scratch.

## Search

Accepts a block height, a block hash, a transaction hash or an address.

## License

MIT
