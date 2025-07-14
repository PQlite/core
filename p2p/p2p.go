package p2p

import (
	"context"
	"log"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func Node() {
	host, err := libp2p.New(
		libp2p.ListenAddrStrings(
			"/ip4/0.0.0.0/tcp/0",
			"/ip6/::/tcp/0"),

		libp2p.NATPortMap(),
		libp2p.EnableHolePunching(),
		libp2p.EnableRelayService(),

		libp2p.EnableAutoNATv2(),
	)
	if err != nil {
		log.Fatal("помилка з p2p ", err)
	}
	defer host.Close()

	log.Printf("Peer ID: %s\n", host.ID())
	log.Println("Listening addresses:")
	for _, addr := range host.Addrs() {
		log.Printf("  %s/p2p/%s\n", addr, host.ID())
	}

	bootstrapPeers := []string{}
	for _, peerAddr := range bootstrapPeers {
		addr, err := peer.AddrInfoFromString(peerAddr)
		if err != nil {
			continue
		}

		go func(addr peer.AddrInfo) {
			if err := host.Connect(context.Background(), addr); err != nil {
				log.Printf("Failed to connect to bootstrap peer: %v\n", err)
			} else {
				log.Printf("Connected to bootstrap peer: %s\n", addr.ID)
			}
		}(*addr)
	}

	select {}
}
