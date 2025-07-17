package chain

import (
	"bytes"
	"sort"
)

type Block struct {
	Height       uint64         // Номер блоку
	Timestamp    int64          // UNIX час
	PrevHash     []byte         // Хеш попереднього блоку
	Hash         []byte         // Хеш цього блоку (розраховується по іншим полям)
	Proposer     string         // Адреса або публічний ключ того, хто створив блок
	Signature    []byte         // Підпис Proposer'а на блоку
	Transactions []*Transaction // Список транзакцій
}

type BlockForSign struct {
	PrevHeight   int
	Timestamp    int64
	PrevHash     []byte
	Proposer     string
	Transactions []*Transaction
}

// sortTransactions сортує транзакції в блоці за їх підписами.
// Це необхідно для детерміністичної серіалізації.
func (b *BlockForSign) sortTransactions() {
	sort.Slice(b.Transactions, func(i, j int) bool {
		return bytes.Compare(b.Transactions[i].Signature, b.Transactions[j].Signature) < 0
	})
}

// func (b *BlockForSign) Sign() (Block, error) {
// 	BlockForSignBytes, err := json.Marshal(*b)
// 	if err != nil {
// 		log.Println("помилка обробки json.Marshal: ", err)
// 		return Block{}, nil
// 	}
// }
