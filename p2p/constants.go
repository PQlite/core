package p2p

import "github.com/libp2p/go-libp2p/core/protocol"

const (
	// wallets
	STAKE        = "stake00000000000000000000000000000000000000000000000"
	REWARDWALLET = "reward"
	REWARD       = float32(1)

	// network
	ns                         = "PQlite_test"
	directProtocol protocol.ID = "/pqlite/direct/1.0.0"
)

var BOOTSTRAPLIST = [2]string{
	"/ip6/2603:c020:8020:57e:39be:e0b6:a47e:c950/udp/4003/quic-v1/p2p/12D3KooWRGYZwViL5uN6qQoadvMSxd7b46ngLbkM2cW2djwemMnc",
	"/ip4/158.180.54.159/udp/4003/quic-v1/p2p/12D3KooWRGYZwViL5uN6qQoadvMSxd7b46ngLbkM2cW2djwemMnc",
}
