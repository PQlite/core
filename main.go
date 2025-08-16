// Package main initializes and runs the PQlite blockchain node, handling the setup of all components.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/PQlite/core/api"
	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/PQlite/core/p2p"
	"github.com/rs/zerolog/log"
)

func main() {
	bs, err := database.InitDB()
	if err != nil {
		log.Fatal().Err(err).Msg("помилка initdb")
	}

	_, err = bs.GetLastBlock()
	if err != nil {
		if err.Error() == "no blocks found" {
			log.Info().Msg("база даних порожня, початок створення genesis блоку")
			b, val := chain.CreateGenesisBlock()
			bs.SaveBlock(&b)
			bs.AddValidator(&val)
			log.Info().Msg("genesis блок створено")
		}
	}

	mempool := chain.Mempool{}
	ctx := context.Background()

	node, err := p2p.NewNode(ctx, &mempool, bs)
	if err != nil {
		log.Fatal().Err(err).Msg("помилка створення p2p ноди")
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
