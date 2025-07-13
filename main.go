package main

import (
	"github.com/PQlite/core/api"
	"github.com/PQlite/core/chain"
)

func main() {
	mempool := &chain.Mempool{}
	api.StartServer(mempool)
}
