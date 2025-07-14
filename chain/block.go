package chain

import (
	"github.com/PQlite/crypto"
)

type Block struct {
	Height       uint64                // Номер блоку
	Timestamp    int64                 // UNIX час
	PrevHash     []byte                // Хеш попереднього блоку
	Hash         []byte                // Хеш цього блоку (розраховується по іншим полям)
	Proposer     string                // Адреса або публічний ключ того, хто створив блок
	Signature    []byte                // Підпис Proposer'а на блоку
	Transactions []*crypto.Transaction // Список транзакцій
}
