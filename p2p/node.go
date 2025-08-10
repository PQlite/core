// Package p2p provides peer-to-peer networking functionality.
package p2p

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	discovery_routing "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
)

type Node struct {
	host         host.Host
	ctx          context.Context
	TxCh         chan *chain.Transaction
	topic        *Topic
	mempool      *chain.Mempool
	bs           *database.BlockStorage
	kdht         *dht.IpfsDHT
	keys         *Keys // NOTE: не думаю, що це гарне рішення, але вже як є
	nextProposer chain.Validator
}

func NewNode(ctx context.Context, mempool *chain.Mempool, bs *database.BlockStorage) (Node, error) {
	var kdht *dht.IpfsDHT

	priv, err := LoadOrCreateIdentity(".node.key")
	if err != nil {
		log.Fatal(err)
	}

	node, err := libp2p.New(
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			kdht, err = dht.New(ctx, h)
			if err != nil {
				return nil, err
			}
			return kdht, nil
		}),

		libp2p.ListenAddrStrings("/ip6/::/udp/4003/quic-v1", "/ip4/0.0.0.0/udp/4003/quic-v1"),
		libp2p.Identity(priv),
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

	keys, err := LoadKeys()
	if err != nil {
		log.Println("помилка завантаження ключів")
		return Node{}, err
	}

	for _, p := range node.Addrs() {
		log.Println(p.String(), node.ID().String())
	}

	return Node{
		host:    node,
		ctx:     ctx,
		TxCh:    make(chan *chain.Transaction),
		topic:   &topic,
		mempool: mempool,
		bs:      bs,
		kdht:    kdht,
		keys:    keys,
	}, nil
}

// Start Запуск p2p сервер
func (n *Node) Start() {
	n.host.SetStreamHandler(directProtocol, n.handleStreamMessages)

	// Підключення до bootstrap
	n.connectingToBootstrap()

	go n.peerDiscovery()
	go n.handleTxCh()
	go n.handleBroadcastMessages()

	n.syncBlockchain()

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

				m := Message{
					Type:      MsgNewTransaction,
					Timestamp: time.Now().UnixMilli(),
					Data:      txBytes,
					Pub:       n.keys.Pub,
				}
				err = m.sign(n.keys.Priv)
				if err != nil {
					log.Println("sing error: ", err)
					continue
				}

				n.topic.broadcast(&m, n.ctx)
			}
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Node) peerDiscovery() {
	ticker := time.NewTicker(120 * time.Second)

	routingDiscovery := discovery_routing.NewRoutingDiscovery(n.kdht)
	util.Advertise(n.ctx, routingDiscovery, ns)

	for {
		select {
		case <-ticker.C:
			peerChan, err := routingDiscovery.FindPeers(n.ctx, ns)
			if err != nil {
				panic(err)
			}

			for p := range peerChan {
				if p.ID != n.host.ID() {
					ch := ping.Ping(n.ctx, n.host, p.ID)
					res := <-ch
					if res.Error == nil {
						log.Println(res.RTT)
					}
				}
			}
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Node) connectingToBootstrap() {
	for _, addr := range BOOTSTRAPLIST {
		pi, err := peer.AddrInfoFromString(addr)
		if err != nil {
			log.Println("помилка отримання адреси bootstrap: ", err)
		}
		err = n.host.Connect(n.ctx, *pi)
		if err != nil {
			log.Println("помилка підключення до bootstrap: ", err)
		} else {
			log.Println("підключено до ", pi.Addrs)
		}
	}
}
