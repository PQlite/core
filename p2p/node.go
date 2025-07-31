package p2p

import (
	"context"
	"log"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	discovery_routing "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
)

func Node(mempool *chain.Mempool, bs *database.BlockStorage) {
	ctx := context.Background()
	var kdht *dht.IpfsDHT

	node, err := libp2p.New(
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			kdht, err = dht.New(ctx, h)
			if err != nil {
				return nil, err
			}
			return kdht, nil
		}),
		libp2p.ListenAddrStrings("/ip6/::/tcp/0"),
	)
	if err != nil {
		panic(err)
	}
	defer node.Close()

	// Підключення до bootstrap
	connectingToBootstrap(node, ctx)

	// discovery
	routingDiscovery := discovery_routing.NewRoutingDiscovery(kdht)
	util.Advertise(ctx, routingDiscovery, "123hello1", discovery.TTL(10*time.Second))

	peerChan, err := routingDiscovery.FindPeers(ctx, "123hello1", discovery.TTL(10*time.Second))
	if err != nil {
		panic(err)
	}

	for p := range peerChan {
		if p.ID == node.ID() {
			continue
		} else {
			log.Println(p.ID)
			ch := ping.Ping(ctx, node, p.ID)
			res := <-ch
			if res.Error != nil {
				log.Println("err")
			} else {
				log.Println(res.RTT)
			}
		}
	}
}

func connectingToBootstrap(node host.Host, ctx context.Context) {
	for _, bootstrap := range dht.DefaultBootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(bootstrap)
		if err != nil {
			log.Println("помилка отримання адреси bootstrap: ", err)
			continue
		}
		err = node.Connect(ctx, *pi)
		if err != nil {
			log.Println("помилка підключення до bootstrap: ", err)
		} else {
			log.Println("підключено до ", pi.Addrs)
		}
	}
}
