package p2p

import (
	"encoding/json"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/crypto"
	"github.com/rs/zerolog/log"
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
	Data      []byte      `json:"data"`      // дані через json.Marshal // NOTE: можливо треба замінити на структуру, якщо так можна, тому що там завжди структури
	Pub       []byte      `json:"pub"`       // публічний коюч відправника, для перевірки
	Signature []byte      `json:"signature"` // підпис відправника
}

// OPTIMIZE: треба видалити, тому що відправка блока 2 рази це не дуже ефективно
type Commit struct {
	Voters []chain.Vote `json:"voters"`
	Block  chain.Block  `json:"block"`
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

func (n *Node) getMsgBlockProposalMsg() (*Message, error) {
	newBlock := n.createNewBlock()

	newBlockBytes, err := json.Marshal(newBlock)
	if err != nil {
		log.Error().Err(err).Msg("помилка розпаковки нового блоку")
		return nil, err
	}

	blockProposalMsg := Message{
		Type:      MsgBlockProposal,
		Timestamp: time.Now().UnixMilli(),
		Data:      newBlockBytes,
		Pub:       n.keys.Pub,
	}

	err = blockProposalMsg.sign(n.keys.Priv)
	if err != nil {
		log.Error().Err(err).Msg("помилка підпису нового блоку")
		return nil, err
	}

	return &blockProposalMsg, nil
}

func (n *Node) getCommitMsg(voters *[]chain.Vote, b *chain.Block) (*Message, error) {
	commit := Commit{
		Voters: *voters,
		Block:  *b,
	}

	commitBytes, err := json.Marshal(commit)
	if err != nil {
		return nil, err
	}

	msg := Message{
		Type:      MsgCommit,
		Timestamp: time.Now().UnixMilli(),
		Data:      commitBytes,
		Pub:       n.keys.Pub,
	}

	if err = msg.sign(n.keys.Priv); err != nil {
		return nil, err
	}

	return &msg, nil
}

// TODO: додати логування
func (n *Node) getVoteMsg(blockBytes []byte) (*Message, error) {
	sig, err := crypto.Sign(n.keys.Priv, blockBytes)
	if err != nil {
		return nil, err
	}

	vote := chain.Vote{
		Pub:       n.keys.Pub,
		Signature: sig,
	}

	voteBytes, err := json.Marshal(vote)
	if err != nil {
		return nil, err
	}

	msg := Message{
		Type:      MsgVote,
		Timestamp: time.Now().UnixMilli(),
		Data:      voteBytes,
		Pub:       n.keys.Pub,
	}

	if err = msg.sign(n.keys.Priv); err != nil {
		return nil, err
	}

	return &msg, nil
}
