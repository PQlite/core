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
	// OPTIMIZE: зробити отримання нових блоків в batch
	for {
		localBlock, err := n.bs.GetLastBlock()
		if err != nil {
			log.Error().Err(err).Msg("помилка отримання останнього блоку при синхронізації")
			return
		}
		if err := n.setNextProposer(); err != nil {
			log.Error().Err(err).Msg("помилка вибору proposer при синхронізації")
			return
		}

		data, err := json.Marshal(chain.Block{Height: localBlock.Height + 1})
		if err != nil {
			log.Error().Err(err).Msg("помилка серіалізації запиту блоку")
			return
		}

		m := Message{
			Type:      MsgRequestBlock,
			Timestamp: time.Now().UnixMilli(),
			Data:      data,
			Pub:       n.keys.Pub,
		}
		if err = m.sign(n.keys.Priv); err != nil {
			log.Error().Err(err).Msg("помилка підпису повідомлення при синхронізації")
			return
		}

		peerForSync := n.chooseRandomPeer()
		if peerForSync == nil {
			log.Warn().Msg("не було знайдено peer для синхронізації")
			return
		}

		respMsg, err := n.sendStreamMessage(*peerForSync, &m)
		if err != nil {
			log.Error().Err(err).Msg("помилка відправки повідомлення при синхронізації")
			return
		}

		var respBlock chain.Block
		if err = json.Unmarshal(respMsg.Data, &respBlock); err != nil {
			log.Error().Err(err).Msg("помилка десеріалізації отриманого блоку")
			return
		}

		// якщо запитаного блоку немає — ланцюжок актуальний
		if respBlock.Height < localBlock.Height+1 {
			log.Info().Msg("blockchain is up to date!")

			if err := n.setNextProposer(); err != nil {
				log.Error().Err(err).Msg("помилка вибору proposer після синхронізації")
				return
			}

			if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
				blockProposalMsg, err := n.getMsgBlockProposalMsg()
				if err != nil {
					log.Error().Err(err).Msg("помилка створення block proposal після синхронізації")
					return
				}

				if err := n.topic.broadcast(blockProposalMsg, n.ctx); err != nil {
					log.Error().Err(err).Msg("помилка broadcast block proposal після синхронізації")
				}
			}
			return
		}

		if err := n.fullBlockVerefication(&respBlock); err != nil {
			log.Error().Err(err).Msg("блок не пройшов верифікацію при синхронізації")
			return
		}
		if err := n.bs.SaveBlock(&respBlock); err != nil {
			log.Error().Err(err).Msg("помилка збереження блоку при синхронізації")
			return
		}
		if err := n.addValidatorsToDB(&respBlock); err != nil {
			log.Error().Err(err).Msg("помилка додавання валідаторів при синхронізації")
			return
		}
		if err := n.updateBalancesNonces(&respBlock); err != nil {
			log.Error().Err(err).Msg("помилка оновлення балансів при синхронізації")
			return
		}
		log.Info().Uint32("height", respBlock.Height).Int64("latency", time.Now().UnixMilli()-respMsg.Timestamp).Msg("додано новий блок до ланцюжка")
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
