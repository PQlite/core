package p2p

import (
	"encoding/json"

	"github.com/PQlite/crypto"
)

type MessageType string

const (
	// Транзакції
	MsgNewTransaction MessageType = "newTransaction"

	// Блоки
	MsgNewBlock     MessageType = "newBlock"
	MsgRequestBlock MessageType = "requestBlock"
	MsgResponeBlock MessageType = "responeBlock"

	// PoS
	MsgBlockProposal MessageType = "blockProposal"
	MsgVote          MessageType = "vote"
	MsgCommit        MessageType = "commit"

	// Валідатори
	MsgValidatorSet MessageType = "validatorSet"
)

type Message struct {
	Type      MessageType `json:"type"` // Тип повідомлення
	Timestamp int64       `json:"timestamp"`
	Data      []byte      `json:"data"`      // дані через json.Marshal
	Pub       []byte      `json:"pub"`       // публічний коюч відправника, для перевірки
	Signature []byte      `json:"signature"` // підпис відправника
}

type UnsignMessage struct {
	Type      MessageType `json:"type"` // Тип повідомлення
	Timestamp int64       `json:"timestamp"`
	Data      []byte      `json:"data"` // дані через json.Marshal
	Pub       []byte      `json:"pub"`  // публічний коюч відправника, для перевірки
}

func (um *UnsignMessage) Sign(priv []byte) (*Message, error) {
	unsignMessageBytes, err := json.Marshal(um)
	if err != nil {
		return &Message{}, err
	}
	sign, err := crypto.Sign(priv, unsignMessageBytes)
	if err != nil {
		return &Message{}, err
	}
	return &Message{
		Type:      um.Type,
		Timestamp: um.Timestamp,
		Data:      um.Data,
		Pub:       um.Pub,
		Signature: sign,
	}, nil
}

func (m *Message) getUnsignMessage() *UnsignMessage {
	return &UnsignMessage{
		Type:      m.Type,
		Timestamp: m.Timestamp,
		Data:      m.Data,
		Pub:       m.Pub,
	}
}
