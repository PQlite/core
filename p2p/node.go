package p2p

import (
	"bufio"
	"bytes"
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
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
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

const (
	ns                         = "PQlite_test"
	directProtocol protocol.ID = "/pqlite/direct/1.0.0"
)

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

		libp2p.ListenAddrStrings("/ip6/::/tcp/4003", "/ip4/0.0.0.0/tcp/4003"),
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

func (n *Node) handleStreamMessages(stream network.Stream) {
	log.Printf("Отримано новий прямий потік від %s", stream.Conn().RemotePeer())
	defer func() {
		// stream.Reset() // NOTE: що воно робить, і яка різниця порівняно з stream.Close()?
		//                         я дізнався що це щось страше
		stream.Close()
	}()

	// Створюємо reader для читання даних з потоку
	reader := bufio.NewReader(stream)
	// Читаємо дані до символу нового рядка. Це простий спосіб розділяти повідомлення.
	reqBytes, err := reader.ReadBytes('\n')
	if err != nil {
		log.Println("Помилка читання з потоку:", err)
		return
	}

	var msg Message
	err = json.Unmarshal(reqBytes, &msg)
	if err != nil {
		log.Println("Помилка розпаковки повідомлення:", err)
		return
	}

	switch msg.Type {
	case MsgRequestBlock: // HACK: ну тут треба точно переписувати, тому що зараз це жахливо
		var data chain.Block
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			log.Println("помилка розпаковки block з запиту на блок")
			return
		}
		lastBlock, err := n.bs.GetLastBlock()
		if err != nil {
			log.Println("помилка бази даних: ", err)
			return
		}
		if lastBlock.Height <= data.Height {
			respBlockBytes, err := json.Marshal(lastBlock)
			if err != nil {
				panic(err)
			}

			respUmsg := UnsignMessage{
				Type:      MsgResponeBlock,
				Timestamp: time.Now().UnixMilli(),
				Data:      respBlockBytes,
				Pub:       n.keys.Pub,
			}

			respMsg, err := respUmsg.sign(n.keys.Priv)
			if err != nil {
				panic(err)
			}

			respBytes, err := json.Marshal(respMsg)
			if err != nil {
				panic(err)
			}
			writer := bufio.NewWriter(stream)
			_, err = writer.Write(append(respBytes, '\n'))
			if err != nil {
				panic(err)
			}
			writer.Flush()
		} else {
			reqBlock, err := n.bs.GetBlock(data.Height)
			if err != nil {
				panic(err)
			}
			reqBlockBytes, err := json.Marshal(reqBlock)
			if err != nil {
				panic(err)
			}

			respUmsg := UnsignMessage{
				Type:      MsgResponeBlock,
				Timestamp: time.Now().UnixMilli(),
				Data:      reqBlockBytes,
				Pub:       n.keys.Pub,
			}
			respMsg, err := respUmsg.sign(n.keys.Priv)
			if err != nil {
				panic(err)
			}

			respBytes, err := json.Marshal(respMsg)
			if err != nil {
				panic(err)
			}

			writer := bufio.NewWriter(stream)
			_, err = writer.Write(append(respBytes, '\n'))
			if err != nil {
				panic(err)
			}
		}
	}
}

func (n *Node) sendStreamMessage(targetPeer peer.ID, msg *Message) (*Message, error) {
	stream, err := n.host.NewStream(n.ctx, targetPeer, directProtocol)
	if err != nil {
		return nil, fmt.Errorf("не вдалося відкрити потік: %w", err)
	}
	defer stream.Close()

	writer := bufio.NewWriter(stream)
	reader := bufio.NewReader(stream)

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	_, err = writer.Write(append(msgBytes, '\n'))
	if err != nil {
		stream.Reset()
		return nil, err
	}
	writer.Flush()

	respBytes, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("не вдалося прочитати відповідь: %w", err)
	}

	var respMsg Message
	if err = json.Unmarshal(respBytes, &respMsg); err != nil {
		return nil, fmt.Errorf("не вдалося розпакувати відповідь: %w", err)
	}
	// TODO: додати respMsg.verify

	return &respMsg, nil
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
			// TODO: додати перевірку автора ( щоб pubkey збігався з тим, хто повинен був робити блок ). І нагороду, яку він собі назначив
			// TODO: видалити транзакції з mempool, якщо вони вже є в блоці
			if block.Verify() {
				n.bs.SaveBlock(&block)
				val, err := n.chooseValidator()
				if err != nil {
					log.Println("помилка вибору наступного валідатора, ", err)
					continue
				}

				// я це і є настпуний валідатор!
				if bytes.Equal(val.Address, n.keys.Pub) {
					newBlock := n.createNewBlock()

					newBlockBytes, err := json.Marshal(newBlock)
					if err != nil {
						panic(err)
					}

					blockProposalUnMsg := UnsignMessage{
						Type:      MsgBlockProposal,
						Timestamp: time.Now().UnixMilli(),
						Data:      newBlockBytes,
						Pub:       n.keys.Pub,
					}

					blockProposalMsg, err := blockProposalUnMsg.sign(n.keys.Priv)
					if err != nil {
						panic(err)
					}

					n.topic.broadcast(blockProposalMsg, n.ctx)

					n.bs.SaveBlock(&newBlock) // NOTE: треба буде переробити, якщо я хочу робити Vote
				}
			}
		}

		latency := time.Now().UnixMilli() - message.Timestamp
		log.Println("oтримано за ", latency, "ms")
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
	pi, err := peer.AddrInfoFromString("/ip6/2603:c020:8020:57e:39be:e0b6:a47e:c950/tcp/4003/p2p/12D3KooWRGYZwViL5uN6qQoadvMSxd7b46ngLbkM2cW2djwemMnc")
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

func (n *Node) syncBlockchain() {
	for {
		//
		localBlockHeight, err := n.bs.GetLastBlock()
		if err != nil {
			localBlockHeight = &chain.Block{Height: 1}
		}

		if localBlockHeight == nil {
			localBlockHeight = &chain.Block{Height: 1}
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
		peerForSync := n.chooseRandomPeer()
		if peerForSync == nil {
			log.Println("не було знайдено peer для синхронізації")
			return
		}
		respMsg, err := n.sendStreamMessage(*peerForSync, signM)
		if err != nil {
			panic(err)
		}

		var respBlock chain.Block
		if err = json.Unmarshal(respMsg.Data, &respBlock); err != nil {
			panic(err)
		}

		// це якщо запитаного блоку не існує. це означає, що локальна база вже актуальна і має останній блок
		if respBlock.Height < localBlockHeight.Height+1 {
			log.Println("blockchain is up to date!")
			nextProposer, err := n.chooseValidator()
			if err != nil {
				log.Println("помилка вибору наступного валідатора:", err)
				return
			}
			n.nextProposer = nextProposer
			return
		}
		if respBlock.Height == localBlockHeight.Height+1 {
			if !respBlock.Verify() {
				log.Println("отриманий блок, не є валідним")
				return // ISSUE: треба зробити вібір іншого вузла, або повтор
			} else {
				log.Println("отримано блок:", respBlock.Height)

				for _, tx := range respBlock.Transactions {
					if bytes.Equal(tx.To, []byte("stake")) {
						validator := chain.Validator{
							Address: tx.PubKey,
							Amount:  tx.Amount,
						}
						n.bs.AddValidator(&validator)
					}
				}

				n.bs.SaveBlock(&respBlock)
			}
		}
	}
}

func (n *Node) chooseRandomPeer() *peer.ID {
	for _, p := range n.host.Peerstore().Peers() {
		if p == n.host.ID() {
			continue
		}
		if n.host.Network().Connectedness(p) != network.Connected {
			continue
		}
		return &p
	}
	return nil
}

func (n *Node) chooseValidator() (chain.Validator, error) {
	lastBlock, err := n.bs.GetLastBlock()
	if err != nil {
		return chain.Validator{}, fmt.Errorf("Помилка отримання останнього блоку: %w", err)
	}
	validators, err := n.bs.GetValidatorsList()
	if err != nil {
		return chain.Validator{}, fmt.Errorf("Помилка отримання списку валідаторів: %w", err)
	}

	nextProposer, err := chain.SelectNextProposer(lastBlock.Hash, *validators)
	if err != nil {
		return chain.Validator{}, err
	}

	return *nextProposer, nil
}

func (n *Node) createNewBlock() chain.Block {
	lastBlock, err := n.bs.GetLastBlock()
	if err != nil {
		// NOTE: може не найкращів варіант
		panic(err)
	}

	for len(n.mempool.TXs) < 1 {
		time.Sleep(100 * time.Millisecond)
	}

	ublock := chain.BlockForSign{
		Height:       lastBlock.Height + 1,
		Timestamp:    time.Now().UnixMilli(),
		PrevHash:     lastBlock.Hash,
		Proposer:     n.keys.Pub,
		Transactions: n.mempool.TXs,
	}

	block, err := ublock.Sign(n.keys.Priv)
	if err != nil {
		panic(err)
	}
	// TODO: очистити mempool
	return block
}
