package p2p

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

func (n *Node) handleStreamMessages(stream network.Stream) {
	log.Printf("Отримано новий прямий потік від %s", stream.Conn().RemoteMultiaddr())
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

			respMsg := Message{
				Type:      MsgResponeBlock,
				Timestamp: time.Now().UnixMilli(),
				Data:      respBlockBytes,
				Pub:       n.keys.Pub,
			}

			err = respMsg.sign(n.keys.Priv)
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

			respMsg := Message{
				Type:      MsgResponeBlock,
				Timestamp: time.Now().UnixMilli(),
				Data:      reqBlockBytes,
				Pub:       n.keys.Pub,
			}

			err = respMsg.sign(n.keys.Priv)
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

	if !respMsg.verify() {
		return nil, fmt.Errorf("повідомлення має не вілідний підпис")
	}

	return &respMsg, nil
}
