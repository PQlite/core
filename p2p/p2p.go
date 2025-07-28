// Package p2p implements the peer-to-peer networking layer, managing connections
// and data synchronization between nodes.
package p2p

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	tcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
)

func Node(mempool *chain.Mempool, bs *database.BlockStorage) {
	// start a libp2p node with default settings
	// TODO: налаштувати протокол (QUIC або TCP) і речі як Relay, natpunching і все таке
	ctx := context.Background()

	var kdht *dht.IpfsDHT

	node, err := libp2p.New(
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			kdht, err = dht.New(ctx, h, dht.Mode(dht.ModeAuto))
			return kdht, err
		}),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0", "/ip6/::/tcp/0"),
		libp2p.NATPortMap(),
		libp2p.EnableRelay(),
		libp2p.EnableHolePunching(),
		libp2p.EnableRelayService(),
	)
	if err != nil {
		panic(err)
	}

	log.Println("Listen addresses:", node.Addrs())
	log.Println("Node id:", node.ID())

	// TODO: замінити на свої bootstrap peers
	bootstrapPeers := dht.DefaultBootstrapPeers

	log.Println("connection to bootstrap peers")
	for _, peerAddr := range bootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(peerAddr)
		if err != nil {
			log.Println("помилка")
			continue
		}
		go func() {
			if err := node.Connect(ctx, *pi); err != nil {
				log.Println("помилка підключення до peer, ", err)
			} else {
				log.Println("підключено до peer ", pi.ID)
			}
		}()
	}

	// go peerDiscovery(ctx, node, kdht)

	// wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	log.Println("Received signal, shutting down...")

	// shut the node down
	if err := node.Close(); err != nil {
		panic(err)
	}
}

// TODO: переписати і зробити адикватний пошук
func peerDiscovery(ctx context.Context, node host.Host, kdht *dht.IpfsDHT) {
}
