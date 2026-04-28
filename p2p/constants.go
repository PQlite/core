package p2p

import "github.com/libp2p/go-libp2p/core/protocol"

const (
	// wallets
	STAKE        = "stake"
	REWARDWALLET = "reward"
	REWARD       = int64(1)

	// network
	ns                         = "PQlite_test"
	directProtocol protocol.ID = "/pqlite/direct/1.0.0"
)

var BOOTSTRAPLIST = [3]string{
	"/ip6/2603:c020:8020:57e:0:8a60:c2f8:951d/tcp/4003/p2p/12D3KooWFkERhFKsJeeH1Dhy4CkiA7GdvkBcYZ8LZNkxxF1yJNoR",
	"/ip4/130.162.221.205/tcp/4003/p2p/12D3KooWFkERhFKsJeeH1Dhy4CkiA7GdvkBcYZ8LZNkxxF1yJNoR",
	"/ip4/192.168.178.24/tcp/4003/p2p/12D3KooWFkERhFKsJeeH1Dhy4CkiA7GdvkBcYZ8LZNkxxF1yJNoR",
}
