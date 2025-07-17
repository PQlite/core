package main

import (
	"github.com/PQlite/core/api"
	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/p2p"
)

func main() {
	mempool := chain.Mempool{}
	go api.StartServer(&mempool)
	p2p.Node()
}
