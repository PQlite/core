# PQlite

A lightweight Proof-of-Stake blockchain node written in Go.

## Architecture

```
┌─────────────┐    ┌─────────────┐    ┌─────────────┐
│   HTTP API  │    │     P2P     │    │   Database  │
│  (Fiber)    │    │  (libp2p)   │    │  (BadgerDB) │
└──────┬──────┘    └──────┬──────┘    └──────┬──────┘
       │                  │                  │
       └──────────────────┼──────────────────┘
                          │
                    ┌─────┴─────┐
                    │   chain/  │
                    │  PoS core │
                    └───────────┘
```

**Packages:**
- `chain/` — block, transaction, validator, and consensus logic
- `p2p/` — libp2p node, GossipSub broadcast, DHT peer discovery, sync
- `database/` — BadgerDB persistence for blocks, wallets, and validators
- `api/` — REST API for submitting transactions and querying state
- `cmd/cli/` — command-line tool for key management and transactions
- `cmd/bench/` — throughput benchmark

## Consensus

PQlite uses weighted Proof-of-Stake with **round-based proposer rotation**:

1. After each block, the next proposer is selected deterministically from `SHA256(blockHash + round)` weighted by stake.
2. The proposer broadcasts a block proposal; validators vote with their stake keys.
3. When `>50%` of total stake votes, the proposer broadcasts a commit and the block is finalized.
4. If the proposer sends an invalid block or doesn't respond within **15 seconds**, validators broadcast `MsgReject`. On receiving a reject, every node increments the round, which selects a different proposer for the same block height — without touching the chain state.

## Running

```bash
go run .
```

The node will:
- Open (or create) a BadgerDB at `/tmp/badger`
- Create a genesis block on first run
- Start the P2P node on port `4003`
- Start the HTTP API on port `8081`
- Serve the **Web Explorer** at `http://localhost:8081/`

The node key is stored in `.node.key`. The validator/signing key is stored in `.env`.

## PQL Precision

PQlite uses fixed-point arithmetic for amounts with **2 decimal places** (kopecks):
- **1.00 PQL** is represented internally as `100`.
- The CLI and API accept decimal values, but the core logic handles them as `int64`.
- `Precision = 100` is defined in `chain/constants.go`.

## API

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Node status |
| `GET` | `/lastBlock` | Latest block |
| `GET` | `/block/:height` | Block by height |
| `GET` | `/blocks` | All blocks |
| `GET` | `/addr/:hex` | Wallet balance and nonce |
| `GET` | `/txs` | Mempool size |
| `GET` | `/nextProposer` | Current expected proposer |
| `GET` | `/currentRound` | Current consensus round |
| `POST` | `/tx` | Submit a signed transaction |

**Transaction format** (`POST /tx`):
```json
{
  "from":      "<base64 public key>",
  "to":        "<base64 public key>",
  "amount":    125,
  "timestamp": 1700000000000,
  "nonce":     3,
  "signature": "<base64 signature>"
}
```
*Note: `amount: 125` represents `1.25 PQL`.*

## CLI

```bash
# Generate a new key pair
go run ./cmd/cli/ keygen -out mykey.json

# Check balance (address in hex)
go run ./cmd/cli/ balance 8e28875a...

# Send a transaction with decimal amount (e.g., 1.5 PQL)
go run ./cmd/cli/ send -key .env -to 8e28875a... -amount 1.5

# Query blocks
go run ./cmd/cli/ blocks
go run ./cmd/cli/ block 1
```

## Throughput test

```bash
# Run benchmark with 1000 txs, 8 parallel workers, 0.1 PQL each
go run ./cmd/bench/ -count 1000 -par 8 -amount 0.1
```

Reports API submission rate (tx/s), time to first block, and effective TPS after block confirmation.

## Stack

| Component | Library |
|-----------|---------|
| P2P networking | [go-libp2p](https://github.com/libp2p/go-libp2p) |
| Pub/sub | go-libp2p-pubsub (GossipSub) |
| DHT discovery | go-libp2p-kad-dht |
| Storage | [BadgerDB v4](https://github.com/dgraph-io/badger) |
| HTTP API | [Fiber v2](https://github.com/gofiber/fiber) |
| Crypto | github.com/PQlite/crypto |
| Logging | [zerolog](https://github.com/rs/zerolog) |
