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
		localBlockHeight, err := n.bs.GetLastBlock()
		if err != nil {
			panic(err)
		}
		if err := n.setNextProposer(); err != nil {
			panic(err)
		}

		data, err := json.Marshal(chain.Block{Height: localBlockHeight.Height + 1})
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
		// TODO: винести в окерму функцію
		if respBlock.Height < localBlockHeight.Height+1 {
			log.Info().Msg("blockchain is up to date!")

			if err := n.setNextProposer(); err != nil {
				panic(err)
			}

			if bytes.Equal(n.nextProposer.Address, n.keys.Pub) {
				blockProposalMsg, err := n.getMsgBlockProposalMsg()
				if err != nil {
					panic(err)
				}

				if err := n.topic.broadcast(blockProposalMsg, n.ctx); err != nil {
					panic(err)
				}
			}
			return
		}

		if err := n.fullBlockVerefication(&respBlock); err != nil {
			log.Fatal().Err(err).Msg("помилочка")
		}
		if err := n.bs.SaveBlock(&respBlock); err != nil {
			panic(err)
		}
		if err := n.addValidatorsToDB(&respBlock); err != nil {
			panic(err)
		}
		if err := n.updateBalancesNonces(&respBlock); err != nil {
			panic(err)
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
