package p2p

import (
	"encoding/json"

	"github.com/PQlite/crypto"
)

type MessageType string

const (
	// Транзакції
	MsgNewTransaction MessageType = "newTransaction" // data - транзакція

	// Блоки
	MsgNewBlock     MessageType = "newBlock"
	MsgRequestBlock MessageType = "requestBlock"
	MsgResponeBlock MessageType = "responeBlock" // data - block

	// Mempool sync
	MsgMempool MessageType = "mempool"

	// PoS
	MsgBlockProposal MessageType = "blockProposal"
	MsgVote          MessageType = "vote"
	MsgCommit        MessageType = "commit"

	// Валідатори
	MsgValidatorSet    MessageType = "validatorSet"
	MsgDeleteValidator MessageType = "deleteValidator" // NOTE: це треба, щоб видаляти валідатора зі списку, якщо він не зробив блок/вимкнувся
)

// NOTE: можливо треба розділити на окремі структури, взалежності від контенту
type Message struct {
	Type      MessageType `json:"type"` // Тип повідомлення
	Timestamp int64       `json:"timestamp"`
	Data      []byte      `json:"data"`      // дані через json.Marshal
	Pub       []byte      `json:"pub"`       // публічний коюч відправника, для перевірки
	Signature []byte      `json:"signature"` // підпис відправника
}

func (m *Message) sign(priv []byte) error {
	unsignMessageBytes, err := json.Marshal(m)
	if err != nil {
		return err
	}
	sign, err := crypto.Sign(priv, unsignMessageBytes)
	if err != nil {
		return err
	}

	m.Signature = sign

	return nil
}

func (m *Message) getUnsignMessage() *Message {
	return &Message{
		Type:      m.Type,
		Timestamp: m.Timestamp,
		Data:      m.Data,
		Pub:       m.Pub,
	}
}

func (m *Message) verify() bool {
	um := m.getUnsignMessage()
	binUm, err := json.Marshal(um)
	if err != nil {
		return false
	}

	if err = crypto.Verify(m.Pub, binUm, m.Signature); err != nil {
		return false
	}
	return true
}
