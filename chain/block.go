// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"bytes"
	"crypto/sha3"
	"encoding/json"
	"sort"

	"github.com/PQlite/crypto"
)

type Block struct {
	Height       uint32         // Номер блоку
	Timestamp    int64          // UNIX час
	PrevHash     []byte         // Хеш попереднього блоку
	Hash         []byte         // Хеш цього блоку (розраховується по іншим полям)
	Proposer     []byte         // Адреса або публічний ключ того, хто створив блок
	Signature    []byte         // Підпис Proposer'а на блоку
	Transactions []*Transaction // Список транзакцій
}

type BlockForSign struct {
	Height       uint32
	Timestamp    int64
	PrevHash     []byte
	Proposer     []byte
	Transactions []*Transaction
}

// sortTransactions сортує транзакції в блоці за їх підписами.
// Це необхідно для детерміністичної серіалізації.
func (b *BlockForSign) sortTransactions() {
	sort.Slice(b.Transactions, func(i, j int) bool {
		return bytes.Compare(b.Transactions[i].Signature, b.Transactions[j].Signature) < 0
	})
}

func (b *BlockForSign) Sign(binPriv []byte) (Block, error) {
	b.sortTransactions()

	BlockForSignBytes, err := json.Marshal(*b)
	if err != nil {
		return Block{}, err
	}
	sig, err := crypto.Sign(binPriv, BlockForSignBytes)
	if err != nil {
		return Block{}, err
	}

	blockHash := sha3.Sum224(BlockForSignBytes)

	return Block{
		Height:       b.Height,
		Timestamp:    b.Timestamp,
		PrevHash:     b.PrevHash,
		Hash:         blockHash[:],
		Proposer:     b.Proposer,
		Signature:    sig,
		Transactions: b.Transactions,
	}, nil
}

func (b *Block) Verify() bool { // XXX: чому воно повертає bool а не error?
	blockForVerify := BlockForSign{
		Height:       b.Height,
		Timestamp:    b.Timestamp,
		PrevHash:     b.PrevHash,
		Proposer:     b.Proposer,
		Transactions: b.Transactions,
	}

	blockForVerify.sortTransactions()

	binBlockForVerify, err := json.Marshal(blockForVerify)
	if err != nil {
		return false
	}

	res, err := crypto.Verify(b.Proposer, binBlockForVerify, b.Signature)
	if err != nil {
		return false
	}
	return res
}

func (b *Block) VerifyTransactions() bool { // XXX: чому воно повертає bool а не error?
	// OPTIMIZE: зробити обробку багатопотоковою
	for _, tx := range b.Transactions {
		err := tx.Verify()
		if err != nil {
			return false
		}
	}
	return true
}
