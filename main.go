// Package main initializes and runs the PQlite blockchain node, handling the setup of all components.
package main

import (
	"log"

	"github.com/PQlite/core/api"
	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/PQlite/core/p2p"
)

func main() {
	bs, err := database.InitDB()
	if err != nil {
		log.Println("помилка initdb ", err)
		return
	}

	mempool := chain.Mempool{}

	go api.StartServer(&mempool, bs)
	p2p.Node(&mempool, bs)
}
