// Package p2p implements the peer-to-peer networking layer, managing connections
// and data synchronization between nodes.
// TODO: треба буде розділити логіку на більшу кількість функцій
package p2p

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	tcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multihash"
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
		if err := node.Connect(ctx, *pi); err != nil {
			log.Println("помилка підключення до peer, ", err)
		} else {
			log.Println("підключено до peer ", pi.ID)
		}
	}

	go func() {
		peerDiscovery(ctx, node, kdht)
		time.Sleep(4 * time.Second)
	}()

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
	const rendezvousString = "PQlite"
	rendezvousCid, err := cid.NewPrefixV1(cid.Raw, multihash.SHA2_256).Sum([]byte(rendezvousString))
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		if err := kdht.Provide(ctx, rendezvousCid, true); err != nil {
			log.Println(err)
		}
	}()
	log.Println("✅ Successfully started advertising!")

	log.Printf("🔎 Searching for other peers under rendezvous string: %s\n", rendezvousString)

	peerChan := kdht.FindProvidersAsync(ctx, rendezvousCid, 0)

	var wg sync.WaitGroup
	for p := range peerChan {
		if p.ID == node.ID() || len(p.Addrs) == 0 {
			continue
		}

		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			log.Printf("💡 Found peer: %s with addresses: %v\n", pi.ID.String(), pi.Addrs)
			err := node.Connect(ctx, pi)
			if err != nil {
				log.Printf("⚠️ Failed to connect to found peer %s: %s\n", pi.ID, err)
			} else {
				log.Printf("🤝 Connected to peer %s\n", pi.ID.String())
			}
		}(p)
	}
	log.Println("пошук закінчено")
}
