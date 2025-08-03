package main

import (
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/PQlite/core/p2p"
)

func main() {
	keys, err := p2p.LoadKeys()
	if err != nil {
		panic(err)
	}
	bs, err := database.InitDB()
	if err != nil {
		panic(err)
	}

	genesisTx := chain.Transaction{
		From:      []byte(""),
		To:        []byte("stake"),
		Amount:    1,
		Timestamp: time.Now().UnixMilli(),
		Nonce:     1,
		PubKey:    keys.Pub,
	}
	err = genesisTx.Sign(keys.Priv)
	if err != nil {
		panic(err)
	}

	// genesis block
	unblock := chain.BlockForSign{
		Height:       1,
		Timestamp:    0,
		PrevHash:     []byte(""),
		Proposer:     keys.Pub,
		Transactions: []*chain.Transaction{&genesisTx},
	}
	block, err := unblock.Sign(keys.Priv)
	if err != nil {
		panic(err)
	}
	bs.SaveBlock(&block)

	validator := chain.Validator{
		Address: keys.Pub,
		Amount:  1,
	}
	bs.AddValidator(&validator)
}
