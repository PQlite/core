// Package main initializes and runs the PQlite blockchain node, handling the setup of all components.
package main

import (
	"context"
	"fmt"
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
			b, val, wallet := chain.CreateGenesisBlock()
			bs.SaveBlock(&b)
			bs.AddValidator(&val)
			bs.UpdateBalance(&wallet)
			log.Info().Msg("genesis блок створено")
		}
	}

	mempool := chain.Mempool{}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	node, err := p2p.NewNode(ctx, &mempool, bs)
	if err != nil {
		log.Fatal().Err(err).Msg("помилка створення p2p ноди")
	}

	server := api.NewServer(&node, &mempool, bs)

	go server.Start()
	go node.Start()

	<-ctx.Done()
	if err := server.Shutdown(); err != nil {
		log.Error().Err(err).Msg("помилка зупинки http серверу")
	}
	if err := bs.Close(); err != nil {
		log.Error().Err(err).Msg("помилка закриття бази даних")
	}
	fmt.Println("Received signal, shutting down...")
}
