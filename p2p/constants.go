package p2p

import "github.com/libp2p/go-libp2p/core/protocol"

const (
	// wallets
	STAKE        = "stake"
	REWARDWALLET = "reward"
	REWARD       = float32(1)

	// network
	ns                         = "PQlite_test"
	directProtocol protocol.ID = "/pqlite/direct/1.0.0"
)

var BOOTSTRAPLIST = [2]string{
	"/ip6/2603:c020:8020:57e:39be:e0b6:a47e:c950/tcp/4003/p2p/12D3KooWC3MYiZuijTDt18e29z8Fh1S7ZzkWzU9JCY7kDpm83RMY",
	"/ip4/158.180.54.159/tcp/4003/p2p/12D3KooWC3MYiZuijTDt18e29z8Fh1S7ZzkWzU9JCY7kDpm83RMY",
}
