package main

import (
	"os"
	"time"

	"github.com/PQlite/core/p2p"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func init() {
	safeWriter := p2p.NewLogWrapper(os.Stderr)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: safeWriter, TimeFormat: time.RFC3339})
}
