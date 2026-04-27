// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
// TODO: треба вирішити, який саме timestamp я використовую для block
package chain

import (
	"bytes"
	"crypto/sha3"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

	if err := b.GenerateHash(); err != nil {
		return err
	}

	// Підписуємо хеш блоку, а не весь JSON
	sig, err := crypto.Sign(binPriv, b.Hash)
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
	// Створюємо копію для перевірки хешу
	blockForVerify := *b
	blockForVerify.Signature = nil
	blockForVerify.Hash = nil

	blockForVerify.sortTransactions()

	// Генеруємо хеш для порівняння
	if err := blockForVerify.GenerateHash(); err != nil {
		log.Error().Err(err).Msg("помилка генерації hash`у блоку")
		return err
	}

	if !bytes.Equal(b.Hash, blockForVerify.Hash) {
		log.Error().Hex("local hash", blockForVerify.Hash).Hex("out hash", b.Hash).Msg("hash перевірочного блоку не збігається")
		return fmt.Errorf("hash`s не збігаються")
	}

	// Перевіряємо підпис, який було накладено на хеш
	if err := crypto.Verify(b.Proposer, b.Hash, b.Signature); err != nil {
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
			log.Error().Err(err).Msg("помилка перевірки підпису транзакції")
			return err
		}

	}
	return nil
}

func (b *Block) MarshalDeterministic() ([]byte, error) {
	// Для хешування нам потрібні всі дані блоку КРІМ Hash та Signature
	type BlockForHashing struct {
		Height       uint32
		Timestamp    int64
		PrevHash     []byte
		Proposer     []byte
		Transactions []*Transaction
	}

	b.sortTransactions()

	data := BlockForHashing{
		Height:       b.Height,
		Timestamp:    b.Timestamp,
		PrevHash:     b.PrevHash,
		Proposer:     b.Proposer,
		Transactions: b.Transactions,
	}

	res, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func CreateGenesisBlock() (Block, Validator, Wallet) {
	genesisReceiverPubKeyBase64 := "jiiHWiJWyBn42vc8qNEdNY04hVysOnWl0Vx5Xb/mdGo="
	pubBytes, err := base64.StdEncoding.DecodeString(genesisReceiverPubKeyBase64)
	if err != nil {
		panic(err)
	}

	valTx := Transaction{
		From:      pubBytes,
		To:        []byte("stake"),
		Amount:    1,
		Timestamp: 0,
		Nonce:     1,
	}
	balanceTx := Transaction{
		From:      []byte("reward"),
		To:        pubBytes,
		Amount:    100000000,
		Timestamp: 0,
		Nonce:     2,
	}

	b := Block{
		Height:       0,
		Transactions: []*Transaction{&valTx, &balanceTx},
	}
	val := Validator{
		Address: pubBytes,
		Amount:  1,
	}
	wallet := Wallet{
		Address: pubBytes,
		Balance: 100000000,
		Nonce:   2,
	}
	return b, val, wallet
}
