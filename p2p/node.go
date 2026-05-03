// Package p2p provides peer-to-peer networking functionality.
package p2p

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	discovery_routing "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/util"
	"github.com/rs/zerolog/log"
)

type Node struct {
	host          host.Host
	ctx           context.Context
	TxCh          chan *chain.Transaction
	topic         *Topic
	mempool       *chain.Mempool
	bs            *database.BlockStorage
	kdht          *dht.IpfsDHT
	keys          *Keys
	nextProposer  chain.Validator
	currentRound  uint32
	vote          chain.VoteCh
	messagesQueue chan Message
	syncing       atomic.Bool
	isProposing   atomic.Bool
	lastBlockTime time.Time
}

// mdnsNotifee підключається до піра щойно він знайдений через mDNS у локальній мережі.
type mdnsNotifee struct {
	h   host.Host
	ctx context.Context
}

func (m *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	log.Info().Str("peer", pi.ID.String()).Msg("mDNS: знайдено пір у локальній мережі")
	if err := m.h.Connect(m.ctx, pi); err != nil {
		log.Debug().Err(err).Str("peer", pi.ID.String()).Msg("mDNS: не вдалося підключитися")
	} else {
		log.Info().Str("peer", pi.ID.String()).Msg("mDNS: підключено")
	}
}

func NewNode(ctx context.Context, mempool *chain.Mempool, bs *database.BlockStorage) (Node, error) {
	var kdht *dht.IpfsDHT

	priv, err := LoadOrCreateIdentity(".node.key")
	if err != nil {
		log.Fatal().Err(err).Msg("помилка завантаження ідентифікатора")
	}

	cm, err := connmgr.NewConnManager(
		100, // Lowwater
		400, // Highwater,
		connmgr.WithGracePeriod(time.Minute),
	)
	if err != nil {
		return Node{}, err
	}

	node, err := libp2p.New(
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			kdht, err = dht.New(ctx, h, dht.Mode(dht.ModeServer))
			if err != nil {
				return nil, err
			}
			return kdht, nil
		}),

		libp2p.ListenAddrStrings("/ip6/::/tcp/4003", "/ip4/0.0.0.0/tcp/4003"),
		libp2p.Identity(priv),
		libp2p.ConnectionManager(cm),
		libp2p.NATPortMap(),
		libp2p.EnableAutoNATv2(),
		libp2p.EnableRelay(),
		libp2p.EnableRelayService(),
		libp2p.EnableHolePunching(),
		libp2p.EnableNATService(),
	)
	if err != nil {
		return Node{}, err
	}

	// mDNS — автоматичне виявлення нод у локальній мережі (LAN/Wi-Fi)
	mdnsService := mdns.NewMdnsService(node, ns, &mdnsNotifee{h: node, ctx: ctx})
	if err := mdnsService.Start(); err != nil {
		log.Error().Err(err).Msg("не вдалося запустити mDNS (продовжуємо без нього)")
	} else {
		log.Info().Msg("mDNS запущено")
	}

	topic, err := topicInit(ctx, node)
	if err != nil {
		return Node{}, err
	}

	keys, err := LoadKeys()
	if err != nil {
		log.Error().Err(err).Msg("помилка завантаження ключів")
		return Node{}, err
	}

	for _, p := range node.Addrs() {
		log.Info().Str("address", p.String()).Str("peer_id", node.ID().String()).Msg("p2p node address")
	}

	lastBlock, err := bs.GetLastBlock()
	lastBlockTime := time.Now()
	if err == nil {
		lastBlockTime = time.UnixMilli(lastBlock.Timestamp)
	}

	return Node{
		host:          node,
		ctx:           ctx,
		TxCh:          make(chan *chain.Transaction),
		topic:         &topic,
		mempool:       mempool,
		bs:            bs,
		kdht:          kdht,
		keys:          keys,
		nextProposer:  chain.Validator{},
		vote:          make(chan chain.Vote, 100),
		messagesQueue: make(chan Message, 100),
		lastBlockTime: lastBlockTime,
	}, nil
}

func (n *Node) Start() {
	// Коли з'являється новий пір (через mDNS, DHT або bootstrap) — ретригеримо синхронізацію.
	// Register notifications BEFORE starting connections to avoid missing initial events.
	n.host.Network().Notify(&libp2pnet.NotifyBundle{
		ConnectedF: func(_ libp2pnet.Network, conn libp2pnet.Conn) {
			log.Debug().Str("peer", conn.RemotePeer().String()).Msg("новий пір — запускаємо синхронізацію")
			go n.syncBlockchain()
		},
	})

	go n.bootstrapLoop()

	go n.peerDiscovery()
	go n.handleBroadcastMessages()
	go n.handleTxCh()
	go n.host.SetStreamHandler(directProtocol, n.handleStreamMessages)

	n.syncBlockchain()

	<-n.ctx.Done()
	log.Info().Msg("отримано команду зупинки в Node")
	if err := n.host.Close(); err != nil {
		log.Error().Err(err).Msg("помилка закриття p2p host")
	}
}

func (n *Node) bootstrapLoop() {
	// First attempt immediately
	n.connectToBootstrap()
	if err := n.kdht.Bootstrap(n.ctx); err != nil {
		log.Error().Err(err).Msg("помилка ініціалізації DHT")
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// If we have few peers, try to reconnect to bootstrap
			if len(n.host.Network().Peers()) < 3 {
				log.Debug().Msg("мало пірів, пробуємо перепідключитися до bootstrap...")
				n.connectToBootstrap()
				if err := n.kdht.Bootstrap(n.ctx); err != nil {
					log.Error().Err(err).Msg("помилка ре-ініціалізації DHT")
				}
			}
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Node) handleTxCh() {
	for {
		select {
		case tx := <-n.TxCh:
			log.Info().Hex("tx_from", tx.From).Msg("отримано нову транзакцію з API")

			// Валідація timestamp
			now := time.Now().UnixMilli()
			if tx.Timestamp > now+60000 {
				log.Warn().Msg("відхилено API: транзакція з майбутнього")
				continue
			}
			if tx.Timestamp < now-86400000 {
				log.Warn().Msg("відхилено API: транзакція занадто стара")
				continue
			}

			// Захист від DOS
			wallet, _ := n.bs.GetWalletByAddress(tx.From)
			if tx.Nonce > wallet.Nonce+10 {
				log.Warn().Uint32("tx_nonce", tx.Nonce).Uint32("wallet_nonce", wallet.Nonce).Msg("відхилено API: Nonce занадто далеко")
				continue
			}

			if err := n.mempool.Add(tx); err != nil {
				log.Error().Err(err).Msg("помилка додавання транзакції в mempool")
			} else {
				txBytes, err := json.Marshal(tx)
				if err != nil {
					log.Error().Err(err).Msg("помилка серіалізації транзакції")
					continue
				}

				m := Message{
					Type:      MsgNewTransaction,
					Timestamp: time.Now().UnixMilli(),
					Data:      txBytes,
					Pub:       n.keys.Pub,
				}
				if err = m.sign(n.keys.Priv); err != nil {
					log.Error().Err(err).Msg("помилка підпису транзакції")
					continue
				}

				if err := n.topic.broadcast(&m, n.ctx); err != nil {
					log.Error().Err(err).Msg("помилка трансляції транзакції")
				}

				if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
					go n.tryProposeBlock()
				}
			}
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Node) peerDiscovery() {
	routingDiscovery := discovery_routing.NewRoutingDiscovery(n.kdht)
	util.Advertise(n.ctx, routingDiscovery, ns)

	// Перший пошук одразу після старту, не чекаємо тікера
	n.findAndConnectPeers(routingDiscovery)

	ticker := time.NewTicker(120 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			n.findAndConnectPeers(routingDiscovery)
		case <-n.ctx.Done():
			return
		}
	}
}

func (n *Node) findAndConnectPeers(rd *discovery_routing.RoutingDiscovery) {
	peerChan, err := rd.FindPeers(n.ctx, ns)
	if err != nil {
		log.Error().Err(err).Msg("помилка пошуку пірів через DHT")
		return
	}
	for p := range peerChan {
		if p.ID == n.host.ID() {
			continue
		}

		if n.host.Network().Connectedness(p.ID) != libp2pnet.Connected {
			log.Info().Str("peer", p.ID.String()).Msg("DHT: знайдено пір, підключаємося...")
			if err := n.host.Connect(n.ctx, p); err != nil {
				log.Debug().Err(err).Str("peer", p.ID.String()).Msg("DHT: не вдалося підключитися")
				continue
			}
			log.Info().Str("peer", p.ID.String()).Msg("DHT: підключено")
		}
	}
}

// GetNextProposer повертає поточного очікуваного творця блоку.
func (n *Node) GetNextProposer() chain.Validator {
	return n.nextProposer
}

// GetCurrentRound повертає номер поточного раунду консенсусу.
func (n *Node) GetCurrentRound() uint32 {
	return n.currentRound
}

func (n *Node) connectToBootstrap() {

	for _, addr := range BOOTSTRAPLIST {
		pi, err := peer.AddrInfoFromString(addr)
		if err != nil {
			log.Error().Err(err).Str("address", addr).Msg("помилка отримання адреси bootstrap")
			continue
		}
		if err = n.host.Connect(n.ctx, *pi); err != nil {
			log.Error().Err(err).Str("address", addr).Msg("помилка підключення до bootstrap")
		} else {
			log.Debug().Str("address", pi.Addrs[0].String()).Msg("підключено до bootstrap")
		}
	}
}
