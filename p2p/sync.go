package p2p

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/rs/zerolog/log"
)

func (n *Node) syncBlockchain() {
	// OPTIMIZE: зробити отримання нових блоків в btach
	for {
		var nextBlockHeight uint32
		localBlockHeight, err := n.bs.GetLastBlock()
		if err != nil || localBlockHeight == nil {
			nextBlockHeight = 0
		} else {
			nextBlockHeight = localBlockHeight.Height + 1
		}

		data, err := json.Marshal(chain.Block{Height: nextBlockHeight})
		if err != nil {
			log.Fatal().Err(err).Msg("помилка розпаковки блоку")
		}

		m := Message{
			Type:      MsgRequestBlock,
			Timestamp: time.Now().UnixMilli(),
			Data:      data,
			Pub:       n.keys.Pub,
		}
		err = m.sign(n.keys.Priv)
		if err != nil {
			log.Fatal().Err(err).Msg("помилка підпису повідомлення")
		}

		peerForSync := n.chooseRandomPeer()
		if peerForSync == nil {
			log.Warn().Msg("не було знайдено peer для синхронізації")
			return
		}
		respMsg, err := n.sendStreamMessage(*peerForSync, &m)
		if err != nil {
			log.Fatal().Err(err).Msg("помилка відправки повідомлення")
		}

		var respBlock chain.Block
		if err = json.Unmarshal(respMsg.Data, &respBlock); err != nil {
			log.Fatal().Err(err).Msg("помилка розпаковки блоку")
		}

		// це якщо запитаного блоку не існує. це означає, що локальна база вже актуальна і має останній блок
		if respBlock.Height < nextBlockHeight {
			log.Info().Msg("blockchain is up to date!")
			nextProposer, err := n.chooseValidator()
			if err != nil {
				log.Error().Err(err).Msg("помилка вибору наступного валідатора")
				return
			}
			n.nextProposer = nextProposer

			if bytes.Equal(nextProposer.Address, n.keys.Pub) {
				block := n.createNewBlock()

				data, err := json.Marshal(block)
				if err != nil {
					log.Fatal().Err(err).Msg("помилка розпаковки блоку")
				}

				msg := Message{
					Type:      MsgBlockProposal,
					Timestamp: time.Now().UnixMilli(),
					Data:      data,
					Pub:       n.keys.Pub,
				}
				err = msg.sign(n.keys.Priv)
				if err != nil {
					log.Fatal().Err(err).Msg("помилка підпису повідомлення")
				}

				n.topic.broadcast(&msg, n.ctx)

			}
			return
		}
		// TODO: додати перевірку балансів, а не тільки підписів
		if respBlock.Height == nextBlockHeight {
			if respBlock.Verify() != nil || respBlock.VerifyTransactions() != nil {
				log.Warn().Msg("отриманий блок/транзакції, не є валідним")
				return // ISSUE: треба зробити вібір іншого вузла, або повтор
			} else {
				log.Info().Uint32("height", respBlock.Height).Int64("latency", time.Now().UnixMilli()-respMsg.Timestamp).Msg("блок отримано")

				// TODO: додавати баланс до валідатора, а не просто перезаписувати
				for _, tx := range respBlock.Transactions {
					if bytes.Equal(tx.To, []byte(STAKE)) {
						validator := chain.Validator{
							Address: tx.PubKey,
							Amount:  tx.Amount,
						}
						n.bs.AddValidator(&validator)
						log.Info().Float32("amount", validator.Amount).Msg("додано валідатора")
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

// func (n *Node) processNewBlock() {}
// TODO: написати це, щоб не було повтору в node.go і sync.go
