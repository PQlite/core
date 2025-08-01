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
	keys    *Keys // NOTE: не думаю, що це гарне рішення, але вже як є
}

const (
	ns = "PQlite_test"
)

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

	keys, err := loadKeys()
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

func (n *Node) handleMessages() {
	// TODO: реалізувати це
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

				um := UnsignMessage{
					Type:      MsgNewTransaction,
					Timestamp: time.Now().UnixMilli(),
					Data:      txBytes,
					Pub:       n.keys.Pub,
				}
				m, err := um.sign(n.keys.Priv)
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
func (n *Node) handleBroadcastMessages() {
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
			continue
		}
		if !message.verify() {
			log.Println("підпис повідомлення not valid")
			continue
		}

		switch message.Type {
		case MsgNewTransaction:
			var tx chain.Transaction
			err = json.Unmarshal(message.Data, &tx)
			if err != nil {
				continue
			}

			err = n.mempool.Add(&tx)
			if err != nil {
				log.Println("отрмана транзакція не була додана до mempool через", err)
			}
		case MsgBlockProposal:
			var block chain.Block
			err = json.Unmarshal(message.Data, &block)
			if err != nil {
				log.Println("помилка розпаковки blockProposal")
				continue
			}

			// NOTE: я ще не впевнений в MsgVote, тому що, якщо я перевірив блок, і він правельний, то це означає, що усі за нього проголосують
			// TODO: додати перевірку автора ( щоб pubkey збігався з тим, хто повинен був робити блок.). І нагороду, яку він собі назначив
			if block.Verify() {
				n.bs.SaveBlock(&block)
			}
		}

		latency := time.Now().UnixMilli() - message.Timestamp
		fmt.Println("Отримано за ", latency, "ms")
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
	// TODO: зробити bootstrap
	// pi, err := peer.AddrInfoFromString("")
	for _, addr := range dht.DefaultBootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(addr)
		if err != nil {
			log.Println("помилка отримання адреси bootstrap: ", err)
		}
		err = n.host.Connect(n.ctx, *pi)
		if err != nil {
			log.Println("помилка підключення до bootstrap: ", err)
		} else {
			log.Println("підключено до ", pi.ID)
		}
	}
}

// NOTE: тут я використав broadcast, що є дуже не дуже для sync блоків
func (n *Node) syncBlockchain() {
	for {
		localBlockHeight, err := n.bs.GetLastBlock()
		if err != nil {
			log.Println("помилка бази даних: ", err)
		}

		data, err := json.Marshal(chain.Block{Height: localBlockHeight.Height + 1})
		if err != nil {
			panic(err)
		}

		m := Message{
			Type:      MsgRequestBlock,
			Timestamp: time.Now().UnixMilli(),
			Data:      data,
			Pub:       n.keys.Pub,
		}
		um := m.getUnsignMessage()
		signM, err := um.sign(n.keys.Priv)
		if err != nil {
			panic(err)
		}
		n.topic.broadcast(signM, n.ctx)
		time.Sleep(1 * time.Second)
	}
}
