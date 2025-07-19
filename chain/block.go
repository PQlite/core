package chain

import (
	"bytes"
	"crypto/sha3"
	"encoding/json"
	"log"
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
	PrevHeight   uint32
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
	BlockForSignBytes, err := json.Marshal(*b)
	if err != nil {
		log.Println("помилка обробки json.Marshal: ", err)
		return Block{}, err
	}
	sig, err := crypto.Sign(binPriv, BlockForSignBytes)
	if err != nil {
		log.Println("err, ", err)
		return Block{}, err
	}

	blockHash := sha3.Sum224(BlockForSignBytes)

	return Block{
		Height:       uint32(b.PrevHeight) + 1,
		Timestamp:    b.Timestamp,
		PrevHash:     b.PrevHash,
		Hash:         blockHash[:],
		Proposer:     b.Proposer,
		Signature:    sig,
		Transactions: b.Transactions,
	}, nil
}
