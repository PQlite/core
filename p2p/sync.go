package p2p

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/rs/zerolog/log"
)

func (n *Node) syncBlockchain() {
	// Гарантуємо що одночасно виконується лише одна синхронізація.
	if !n.syncing.CompareAndSwap(false, true) {
		return
	}
	defer n.syncing.Store(false)

	localBlock, _ := n.bs.GetLastBlock()
	targetHeight := n.fetchMaxTargetHeight()

	pb := &ProgressBar{
		Total:   int(targetHeight),
		Current: int(localBlock.Height),
	}
	GlobalProgressBar = pb

	syncStarted := false

	for {
		localBlock, err := n.bs.GetLastBlock()
		if err != nil {
			log.Error().Err(err).Msg("помилка отримання останнього блоку при синхронізації")
			return
		}

		// Якщо ми отримали блоки, але Total все ще 0 або менше Current, пробуємо оновити
		if pb.Total <= pb.Current {
			targetHeight = n.fetchMaxTargetHeight()
			if targetHeight > uint32(pb.Total) {
				pb.Total = int(targetHeight)
			}
		}

		pb.Current = int(localBlock.Height)
		pb.Render()

		peerForSync := n.chooseRandomPeer()
		if peerForSync == nil {
			if syncStarted {
				pb.Finish()
			}
			return
		}

		// Запитуємо batch блоків
		req := RequestBlocks{
			FromHeight: localBlock.Height + 1,
			Count:      100,
		}
		data, _ := json.Marshal(req)

		m := Message{
			Type:      MsgRequestBlock,
			Timestamp: time.Now().UnixMilli(),
			Data:      data,
			Pub:       n.keys.Pub,
		}
		if err = m.sign(n.keys.Priv); err != nil {
			return
		}

		respMsg, err := n.sendStreamMessage(*peerForSync, &m)
		if err != nil {
			log.Debug().Err(err).Str("peer", peerForSync.String()).Msg("помилка отримання блоків від піра")
			continue
		}

		var blocks []chain.Block
		var respBatch ResponseBlocks
		if err := json.Unmarshal(respMsg.Data, &respBatch); err == nil && len(respBatch.Blocks) > 0 {
			blocks = respBatch.Blocks
		} else {
			var singleBlock chain.Block
			if err := json.Unmarshal(respMsg.Data, &singleBlock); err == nil {
				if singleBlock.Height >= localBlock.Height+1 {
					blocks = append(blocks, singleBlock)
				}
			}
		}

		if len(blocks) == 0 {
			if syncStarted {
				pb.Current = pb.Total
				pb.Render()
				pb.Finish()
			}
			log.Info().Msg("blockchain is up to date!")
			n.ResetRound()
			return
		}

		syncStarted = true
		for _, b := range blocks {
			if err := n.processSyncedBlock(b); err != nil {
				log.Error().Err(err).Uint32("height", b.Height).Msg("помилка обробки синхронізованого блоку")
				return
			}
			pb.Current = int(b.Height)
			pb.Render()
		}
	}
}
func (n *Node) fetchMaxTargetHeight() uint32 {
	var maxHeight uint32
	peers := n.host.Network().Peers()

	// Обмежуємо кількість пірів для запиту, щоб не спамити
	count := 0
	for _, p := range peers {
		if count > 5 {
			break
		}
		if n.host.Network().Connectedness(p) != network.Connected {
			continue
		}

		h := n.fetchTargetHeight(p)
		if h > maxHeight {
			maxHeight = h
		}
		count++
	}
	return maxHeight
}

func (n *Node) fetchTargetHeight(p peer.ID) uint32 {
	m := Message{
		Type:      MsgRequestLastBlock,
		Timestamp: time.Now().UnixMilli(),
		Pub:       n.keys.Pub,
	}
	if err := m.sign(n.keys.Priv); err != nil {
		return 0
	}

	resp, err := n.sendStreamMessage(p, &m)
	if err != nil {
		log.Debug().Err(err).Str("peer", p.String()).Msg("пір не підтримує MsgRequestLastBlock або сталася помилка")
		return 0
	}

	var batch ResponseBlocks
	if err := json.Unmarshal(resp.Data, &batch); err == nil && len(batch.Blocks) > 0 {
		return batch.Blocks[0].Height
	}
	return 0
}

func (n *Node) processSyncedBlock(b chain.Block) error {
	localBlock, _ := n.bs.GetLastBlock()
	if b.Height != localBlock.Height+1 {
		return fmt.Errorf("невірна висота синхронізованого блоку: очікувано %d, отримано %d", localBlock.Height+1, b.Height)
	}

	if err := n.fullBlockVerefication(&b); err != nil {
		return err
	}
	if err := n.bs.SaveBlock(&b); err != nil {
		return err
	}
	n.lastBlockTime = time.Now()

	n.applyPenalties(&b)
	n.addValidatorsToDB(&b)
	n.deleteValidatorsFromDB(&b)
	n.updateBalancesNonces(&b)

	return nil
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
