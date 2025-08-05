package p2p

import (
	"bytes"
	"encoding/json"
	"log"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (n *Node) syncBlockchain() {
	for {
		//
		localBlockHeight, err := n.bs.GetLastBlock()
		if err != nil {
			localBlockHeight = &chain.Block{Height: 0}
		}

		if localBlockHeight == nil {
			localBlockHeight = &chain.Block{Height: 0}
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
		err = m.sign(n.keys.Priv)
		if err != nil {
			panic(err)
		}

		peerForSync := n.chooseRandomPeer()
		if peerForSync == nil {
			log.Println("не було знайдено peer для синхронізації")
			return
		}
		respMsg, err := n.sendStreamMessage(*peerForSync, &m)
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

			if bytes.Equal(nextProposer.Address, n.keys.Pub) {
				block := n.createNewBlock()

				data, err := json.Marshal(block)
				if err != nil {
					panic(err)
				}

				msg := Message{
					Type:      MsgBlockProposal,
					Timestamp: time.Now().UnixMilli(),
					Data:      data,
					Pub:       n.keys.Pub,
				}
				err = msg.sign(n.keys.Priv)
				if err != nil {
					panic(err)
				}

				n.topic.broadcast(&msg, n.ctx)

			}
			return
		}
		if respBlock.Height == localBlockHeight.Height+1 {
			if !respBlock.Verify() {
				log.Println("отриманий блок, не є валідним")
				return // ISSUE: треба зробити вібір іншого вузла, або повтор
			} else {
				log.Printf("блок %d отримано за %d ms", respBlock.Height, time.Now().UnixMilli()-respMsg.Timestamp)

				for _, tx := range respBlock.Transactions {
					if bytes.Equal(tx.To, []byte(STAKE)) {
						validator := chain.Validator{
							Address: tx.PubKey,
							Amount:  tx.Amount,
						}
						n.bs.AddValidator(&validator)
						log.Println("додано валідатора:", validator.Amount)
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
