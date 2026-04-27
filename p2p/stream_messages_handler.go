package p2p

import (
	"bufio"
	"encoding/json"
	"fmt"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/rs/zerolog/log"
)

func (n *Node) handleStreamMessages(stream network.Stream) {
	stream.SetReadDeadline(time.Now().Add(5 * time.Second))

	log.Info().Str("from", stream.Conn().RemoteMultiaddr().String()).Msg("Отримано новий прямий потік")

	defer stream.Close()

	reader := bufio.NewReader(stream)
	reqBytes, err := reader.ReadBytes('\n')
	if err != nil {
		log.Error().Err(err).Msg("Помилка читання з потоку")
		return
	}

	var msg Message
	if err = json.Unmarshal(reqBytes, &msg); err != nil {
		log.Error().Err(err).Msg("Помилка розпаковки повідомлення")
		return
	}

	switch msg.Type {
	case MsgRequestBlock:
		n.handleStreamRequestBlock(stream, &msg)
	}
}

func (n *Node) handleStreamRequestBlock(stream network.Stream, msg *Message) {
	var reqData chain.Block
	if err := json.Unmarshal(msg.Data, &reqData); err != nil {
		log.Error().Err(err).Msg("помилка розпаковки block з запиту на блок")
		return
	}

	lastBlock, err := n.bs.GetLastBlock()
	if err != nil {
		log.Error().Err(err).Msg("помилка отримання останнього блоку")
		return
	}

	var blockToSend *chain.Block
	if lastBlock.Height <= reqData.Height {
		blockToSend = lastBlock
	} else {
		blockToSend, err = n.bs.GetBlock(reqData.Height)
		if err != nil {
			log.Error().Err(err).Uint32("height", reqData.Height).Msg("помилка отримання блоку")
			return
		}
	}

	if err := n.writeBlockToStream(stream, blockToSend); err != nil {
		log.Error().Err(err).Msg("помилка відправки блоку в потік")
	}
}

func (n *Node) writeBlockToStream(stream network.Stream, block *chain.Block) error {
	blockBytes, err := json.Marshal(block)
	if err != nil {
		return fmt.Errorf("помилка серіалізації блоку: %w", err)
	}

	respMsg := Message{
		Type:      MsgResponeBlock,
		Timestamp: time.Now().UnixMilli(),
		Data:      blockBytes,
		Pub:       n.keys.Pub,
	}

	if err = respMsg.sign(n.keys.Priv); err != nil {
		return fmt.Errorf("помилка підпису відповіді: %w", err)
	}

	respBytes, err := json.Marshal(respMsg)
	if err != nil {
		return fmt.Errorf("помилка серіалізації відповіді: %w", err)
	}

	writer := bufio.NewWriter(stream)
	if _, err = writer.Write(append(respBytes, '\n')); err != nil {
		return fmt.Errorf("помилка запису в потік: %w", err)
	}
	return writer.Flush()
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

	if _, err = writer.Write(append(msgBytes, '\n')); err != nil {
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

	if !respMsg.verify() {
		return nil, fmt.Errorf("повідомлення має невалідний підпис")
	}

	return &respMsg, nil
}
