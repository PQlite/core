// Package main initializes and runs the PQlite blockchain node, handling the setup of all components.
// TODO: треба вирішити, який саме timestamp я використовую для block
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	ctx := context.Background()

	node, err := p2p.NewNode(ctx, &mempool, bs)
	if err != nil {
		panic(err)
	}

	server := api.NewServer(&node, &mempool, bs)

	go server.Start()
	go node.Start()

	// wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx.Done()
	fmt.Println("Received signal, shutting down...")
}
