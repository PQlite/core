package p2p

import "github.com/libp2p/go-libp2p/core/protocol"

const (
	// wallets
	STAKE         = "stake00000000000000000000000000000000000000000000000"
	REWARD_WALLET = "reward"
	REWARD        = float32(1)

	// network
	ns                         = "PQlite_test"
	directProtocol protocol.ID = "/pqlite/direct/1.0.0"
)
