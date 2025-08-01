package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/PQlite/crypto"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing" discovery_routing "github.com/libp2p/go-libp2p/p2p/discovery/routing"
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

		libp2p.ListenAddrStrings("/ip6/::/tcp/0", "/ip4/0.0.0.0/tcp/0"),
		// NAT traversal (UPnP, NAT-PMP, AutoNAT)
		libp2p.NATPortMap(), // Пробує пробросити порт (UPnP/NAT-PMP)
		// TODO: реалізувати цю функцію
		// libp2p.EnableAutoRelayWithPeerSource(DHTPeerSource(kdht)),
		libp2p.EnableAutoNATv2(), // Дозволяє тобі самому бути relay source (для AutoNAT)

		// Relay
		libp2p.EnableRelay(), // Дозволити relay (старий механізм, потрібний для AutoRelay)
		libp2p.EnableRelayService(),

		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(),
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

// Start Запуск p2p сервер
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
				log.Println("помилка додавання транзакції в mempool: ", err)
			} else {
				txBytes, err := json.Marshal(tx)
				if err != nil {
					log.Println(err)
					continue
				}

				// TODO: треба зробити адекватоно, а це тільки для тесту
				pub, priv, err := crypto.Create()
				if err != nil {
					log.Println(err)
					continue
				}
				binPriv, err := priv.MarshalBinary()
				if err != nil {
					log.Println(err)
					continue
				}
				binPub, err := pub.MarshalBinary()
				if err != nil {
					log.Println(err)
					continue
				}
				//////////////////////////////////////////////

				um := UnsignMessage{
					Type:      MsgNewTransaction,
					Timestamp: time.Now().UnixMilli(),
					Data:      txBytes,
					Pub:       binPub,
				}
				m, err := um.Sign(binPriv)
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

		if msg.ReceivedFrom == n.host.ID() {
			log.Println("повідомлення від себе")
			continue
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
	ticker := time.NewTicker(120 * time.Second)

	routingDiscovery := discovery_routing.NewRoutingDiscovery(kdht)
	util.Advertise(ctx, routingDiscovery, "123hello1")

	for {
		select {
		case <-ticker.C:

			peerChan, err := routingDiscovery.FindPeers(ctx, "123hello1")
			if err != nil {
				panic(err)
			}

			for p := range peerChan {
					if p.ID != node.ID() {
						ch := ping.Ping(ctx, node, p.ID)
						res := <-ch
						if res.Error == nil {
							log.Println(res.RTT)
						}
					}
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
