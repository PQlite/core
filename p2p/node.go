package p2p

import (
	"context"
	"encoding/json"
	"fmt"
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

type Node struct {
	host    host.Host
	ctx     context.Context
	TxCh    chan *chain.Transaction
	topic   *Topic
	mempool *chain.Mempool
	bs      *database.BlockStorage
	kdht    *dht.IpfsDHT
}

func NewNode(ctx context.Context, mempool *chain.Mempool, bs *database.BlockStorage) (Node, error) {
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
		return Node{}, err
	}

	// init topic
	topic, err := topicInit(ctx, node)
	if err != nil {
		return Node{}, err
	}

	return Node{
		host:    node,
		ctx:     ctx,
		TxCh:    make(chan *chain.Transaction),
		topic:   &topic,
		mempool: mempool,
		bs:      bs,
		kdht:    kdht,
	}, nil
}

// Запуск p2p сервер
func (n *Node) Start() {
	// Підключення до bootstrap
	connectingToBootstrap(n.host, n.ctx)

	go peerDiscovery(n.host, n.ctx, n.kdht)
	go n.handleTxCh()
	go n.handleMessages()

	<-n.ctx.Done()
	n.host.Close()
	log.Println("отримано команду зупинки в Node")
}

func (n *Node) handleTxCh() {
	for {
		select {
		case tx := <-n.TxCh:
			log.Printf("Received new transaction %x from API", tx.From)

			if err := n.mempool.Add(tx); err != nil {
				log.Println("помилка додавання транзакції в mempool")
			} else {
				txBytes, err := json.Marshal(tx)
				if err != nil {
					log.Println(err)
					continue
				}

				um := UnsignMessage{
					Type:      MsgNewTransaction,
					Timestamp: time.Now().UnixMilli(),
					Data:      txBytes,
					Pub:       []byte("pub"),
				}
				m, err := um.Sign([]byte("priv"))
				if err != nil {
					log.Println("sing error: ", err)
					continue
				}

				n.topic.broadcast(m, n.ctx)
			}
		case <-n.ctx.Done():
			return
		}
	}
}

// Читання вхідних повідомлень
func (n *Node) handleMessages() {
	for {
		msg, err := n.topic.sub.Next(n.ctx)
		if err != nil {
			log.Println("помилка при отриманні повідомлення: ", err)
		}

		var message Message
		err = json.Unmarshal(msg.Data, &message)
		if err != nil {
			log.Println("помилка розпаковки повідомлення: ", err)
		}
		switch message.Type {
		case MsgNewTransaction:
			log.Println("транзакція")
		}
		latency := time.Now().UnixMilli() - message.Timestamp

		fmt.Println("Отримано за ", latency, "ms")
	}
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
