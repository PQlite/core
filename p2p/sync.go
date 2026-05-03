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

	peerForSync := n.chooseRandomPeer()
	if peerForSync == nil {
		return
	}

	// Запитуємо останній блок піра, щоб знати ціль
	targetHeight := n.fetchTargetHeight(*peerForSync)
	localBlock, _ := n.bs.GetLastBlock()
	
	var pb *ProgressBar
	if targetHeight > localBlock.Height {
		pb = &ProgressBar{
			Total:   int(targetHeight),
			Current: int(localBlock.Height),
		}
		fmt.Fprintln(os.Stderr, "Starting synchronization...")
	} else {
		log.Debug().Uint32("target", targetHeight).Uint32("local", localBlock.Height).Msg("не вдалося визначити цільову висоту або ми вже актуальні")
	}

	for {
		localBlock, err := n.bs.GetLastBlock()
		if err != nil {
			log.Error().Err(err).Msg("помилка отримання останнього блоку при синхронізації")
			return
		}

		if pb != nil {
			pb.Current = int(localBlock.Height)
			pb.Render()
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
			log.Debug().Err(err).Msg("помилка отримання блоків від піра")
			return
		}

		var blocks []chain.Block
		// Спробуємо розпакувати як ResponseBlocks (batch)
		var respBatch ResponseBlocks
		if err := json.Unmarshal(respMsg.Data, &respBatch); err == nil && len(respBatch.Blocks) > 0 {
			blocks = respBatch.Blocks
		} else {
			// Якщо не вийшло — можливо це стара нода повернула один блок
			var singleBlock chain.Block
			if err := json.Unmarshal(respMsg.Data, &singleBlock); err == nil {
				if singleBlock.Height >= localBlock.Height+1 {
					blocks = append(blocks, singleBlock)
				}
			}
		}

		// якщо запитаних блоків немає — ланцюжок актуальний
		if len(blocks) == 0 {
			if pb != nil {
				pb.Current = pb.Total
				pb.Render()
			}
			log.Info().Msg("blockchain is up to date!")
			n.ResetRound()
			return
		}

		for _, b := range blocks {
			if err := n.processSyncedBlock(b); err != nil {
				log.Error().Err(err).Uint32("height", b.Height).Msg("помилка обробки синхронізованого блоку")
				return
			}
			if pb != nil {
				pb.Current = int(b.Height)
				pb.Render()
			}
		}
	}
}

func (n *Node) fetchTargetHeight(p peer.ID) uint32 {
	m := Message{
		Type:      MsgRequestLastBlock,
		Timestamp: time.Now().UnixMilli(),
		Pub: n.keys.Pub,
	}
	if err := m.sign(n.keys.Priv); err != nil {
		return 0
	}

	resp, err := n.sendStreamMessage(p, &m)
	if err != nil {
		// Якщо пір не підтримує MsgRequestLastBlock, пробуємо через MsgRequestBlock останнього можливого
		log.Debug().Err(err).Msg("MsgRequestLastBlock не підтримується піром")
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
	
	log.Debug().Uint32("height", b.Height).Msg("додано новий блок до ланцюжка (sync)")
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
