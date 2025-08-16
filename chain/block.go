// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
// TODO: треба вирішити, який саме timestamp я використовую для block
package chain

import (
	"bytes"
	"crypto/sha3"
	"encoding/base64"
	"encoding/json"
	"sort"

	"github.com/PQlite/crypto"
	"github.com/rs/zerolog/log"
)

type Block struct {
	Height       uint32         // Номер блоку
	Timestamp    int64          // UNIX час
	PrevHash     []byte         // Хеш попереднього блоку
	Hash         []byte         // Хеш цього блоку (розраховується по іншим полям)
	Proposer     []byte         // Адреса або публічний ключ того, хто створив блок
	Transactions []*Transaction // Список транзакцій
	Signature    []byte         // Підпис Proposer'а на блоку
}

// sortTransactions сортує транзакції в блоці за їх підписами.
// Це необхідно для детерміністичної серіалізації.
func (b *Block) sortTransactions() {
	sort.Slice(b.Transactions, func(i, j int) bool {
		return bytes.Compare(b.Transactions[i].Signature, b.Transactions[j].Signature) < 0
	})
}

func (b *Block) Sign(binPriv []byte) error {
	b.sortTransactions()

	BlockForSignBytes, err := json.Marshal(*b)
	if err != nil {
		return err
	}
	sig, err := crypto.Sign(binPriv, BlockForSignBytes)
	if err != nil {
		return err
	}

	b.Signature = sig

	return nil
}

func (b *Block) GenerateHash() error {
	blockBytes, err := b.MarshalDeterministic()
	if err != nil {
		return err
	}

	blockHash := sha3.Sum224(blockBytes)

	b.Hash = blockHash[:]

	return nil
}

func (b *Block) Verify() error {
	// TODO: додати перевірку hash
	// NOTE: можна зробити і краще
	//       скопіювати b і видалити hash, signature
	blockForVerify := Block{
		Height:       b.Height,
		Timestamp:    b.Timestamp,
		PrevHash:     b.PrevHash,
		Proposer:     b.Proposer,
		Transactions: b.Transactions,
	}

	blockForVerify.sortTransactions()

	binBlockForVerify, err := json.Marshal(blockForVerify)
	if err != nil {
		log.Error().Err(err).Msg("помилка json.Marshal в verify")
		return err
	}

	if err = crypto.Verify(b.Proposer, binBlockForVerify, b.Signature); err != nil {
		log.Error().Err(err).Msg("помилка перевірки підпису блоку")
		return err
	}

	return nil
}

func (b *Block) VerifyTransactions() error {
	// OPTIMIZE: зробити обробку багатопотоковою
	for _, tx := range b.Transactions {
		err := tx.Verify()
		if err != nil {
			log.Error().Err(err).Msg("помилка перевірки підписку транзакцій")
			return err
		}
	}
	return nil
}

func (b Block) MarshalDeterministic() ([]byte, error) {
	b.sortTransactions()

	res, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func CreateGenesisBlock() (Block, Validator) {
	genesisReceiverPubKeyBase64 := "jiiHWiJWyBn42vc8qNEdNY04hVysOnWl0Vx5Xb/mdGo="
	pubBytes, err := base64.StdEncoding.DecodeString(genesisReceiverPubKeyBase64)
	if err != nil {
		panic(err)
	}

	b := Block{
		Height: 0,
	}
	val := Validator{
		Address: pubBytes,
		Amount:  1,
	}
	return b, val
}
