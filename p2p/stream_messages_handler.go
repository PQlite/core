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
	log.Info().Str("from", stream.Conn().RemoteMultiaddr().String()).Msg("Отримано новий прямий потік")
	defer func() {
		// stream.Reset() // NOTE: що воно робить, і яка різниця порівняно з stream.Close()?
		//                         я дізнався що це щось страшне
		stream.Close()
	}()

	// Створюємо reader для читання даних з потоку
	reader := bufio.NewReader(stream)
	// Читаємо дані до символу нового рядка. Це простий спосіб розділяти повідомлення.
	reqBytes, err := reader.ReadBytes('\n')
	if err != nil {
		log.Error().Err(err).Msg("Помилка читання з потоку")
		return
	}

	var msg Message
	err = json.Unmarshal(reqBytes, &msg)
	if err != nil {
		log.Error().Err(err).Msg("Помилка розпаковки повідомлення")
		return
	}

	switch msg.Type {
	case MsgRequestBlock: // HACK: ну тут треба точно переписувати, тому що зараз це жахливо
		var data chain.Block
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			log.Error().Err(err).Msg("помилка розпаковки block з запиту на блок")
			return
		}
		lastBlock, err := n.bs.GetLastBlock()
		if err != nil {
			log.Error().Err(err).Msg("помилка бази даних")
			return
		}
		if lastBlock.Height <= data.Height {
			respBlockBytes, err := json.Marshal(lastBlock)
			if err != nil {
				log.Fatal().Err(err).Msg("помилка розпаковки останнього блоку")
			}

			respMsg := Message{
				Type:      MsgResponeBlock,
				Timestamp: time.Now().UnixMilli(),
				Data:      respBlockBytes,
				Pub:       n.keys.Pub,
			}

			err = respMsg.sign(n.keys.Priv)
			if err != nil {
				log.Fatal().Err(err).Msg("помилка підпису повідомлення")
			}

			respBytes, err := json.Marshal(respMsg)
			if err != nil {
				log.Fatal().Err(err).Msg("помилка розпаковки повідомлення")
			}
			writer := bufio.NewWriter(stream)
			_, err = writer.Write(append(respBytes, '\n'))
			if err != nil {
				log.Fatal().Err(err).Msg("помилка запису в потік")
			}
			writer.Flush()
		} else {
			reqBlock, err := n.bs.GetBlock(data.Height)
			if err != nil {
				log.Fatal().Err(err).Msg("помилка отримання блоку")
			}
			reqBlockBytes, err := json.Marshal(reqBlock)
			if err != nil {
				log.Fatal().Err(err).Msg("помилка розпаковки блоку")
			}

			respMsg := Message{
				Type:      MsgResponeBlock,
				Timestamp: time.Now().UnixMilli(),
				Data:      reqBlockBytes,
				Pub:       n.keys.Pub,
			}

			err = respMsg.sign(n.keys.Priv)
			if err != nil {
				log.Fatal().Err(err).Msg("помилка підпису повідомлення")
			}

			respBytes, err := json.Marshal(respMsg)
			if err != nil {
				log.Fatal().Err(err).Msg("помилка розпаковки повідомлення")
			}

			writer := bufio.NewWriter(stream)
			_, err = writer.Write(append(respBytes, '\n'))
			if err != nil {
				log.Fatal().Err(err).Msg("помилка запису в потік")
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

	if !respMsg.verify() {
		return nil, fmt.Errorf("повідомлення має не вілідний підпис")
	}

	return &respMsg, nil
}

