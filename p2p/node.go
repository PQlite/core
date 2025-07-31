package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
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
	messageText := "second"

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
	go peerDiscovery(node, ctx, kdht)

	// init topic
	topic, err := topicInit(ctx, node)
	if err != nil {
		panic(err)
	}
	// TODO: зробити справжню відправку повідомлень
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for {
			<-ticker.C
			message := Message{Text: messageText, Timestamp: time.Now().UnixMilli()}
			topic.broadcast(message, ctx)
		}
	}()

	// Читання вхідних повідомлень
	// TODO: Зробити обробку прийнятих даних
	go func() {
		for {
			msg, err := topic.sub.Next(ctx)
			if err != nil {
				panic(err)
			}

			var data Message
			err = json.Unmarshal(msg.Data, &data)
			if err != nil {
				panic(err)
			}
			if data.Text == messageText {
				continue
			}
			latency := time.Now().UnixMilli() - data.Timestamp

			fmt.Println("Отримано:", data.Text, " за ", latency, "ms")
		}
	}()

	// wait for a SIGINT or SIGTERM signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	fmt.Println("Received signal, shutting down...")
}

func peerDiscovery(node host.Host, ctx context.Context, kdht *dht.IpfsDHT) {
	ticker := time.NewTicker(10 * time.Second) // TODO: збільшити значення на релізі

	routingDiscovery := discovery_routing.NewRoutingDiscovery(kdht)
	util.Advertise(ctx, routingDiscovery, "123hello1", discovery.TTL(10*time.Second))

	for {
		select {
		case <-ticker.C:

			peerChan, err := routingDiscovery.FindPeers(ctx, "123hello1", discovery.TTL(10*time.Second))
			if err != nil {
				panic(err)
			}

			for p := range peerChan {
				go func() {
					if p.ID == node.ID() {
						return
					} else {
						log.Println(p.ID)
						ch := ping.Ping(ctx, node, p.ID)
						res := <-ch
						if res.Error == nil {
							log.Println(res.RTT)
						}
					}
				}()
			}
		case <-ctx.Done():
			return
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
