package main

import (
	"github.com/PQlite/core/api"
	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/p2p"
)

func main() {
	p2p.Node()

	mempool := &chain.Mempool{}
	api.StartServer(mempool)
}
