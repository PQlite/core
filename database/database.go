// Package database тут усе для роботи з базою даних
// TODO: треба зрозуміти, як працювати з таблицями в цій базі
package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/PQlite/core/chain"
	"github.com/dgraph-io/badger/v4"
)

type BlockStorage struct {
	db *badger.DB
}

func InitDB() (*BlockStorage, error) {
	dbExists := checkDBExists("/tmp/badger")
	db, err := badger.Open(badger.DefaultOptions("/tmp/badger"))
	bs := &BlockStorage{db: db}
	if err != nil {
		return nil, err
	}
	if dbExists {
		return bs, nil
	} else {
		// genesis block
		block := chain.Block{
			Height:       0,
			Timestamp:    0,
			PrevHash:     []byte(""),
			Hash:         []byte(""),
			Proposer:     []byte(""),
			Signature:    []byte(""),
			Transactions: []*chain.Transaction{},
		}
		bs.SaveBlock(&block)
	}
	return bs, nil
}

func (bs *BlockStorage) Close() error {
	return bs.db.Close()
}

func (bs *BlockStorage) SaveBlock(block *chain.Block) error {
	height := fmt.Sprintf("block:%d", block.Height)
	data, err := json.Marshal(block)
	if err != nil {
		return err
	}

	txn := bs.db.NewTransaction(true)
	defer txn.Discard()

	err = txn.Set([]byte(height), data)
	if err != nil {
		return err
	}

	if err = txn.Commit(); err != nil {
		return err
	}
	return nil
}

func (bs *BlockStorage) GetBlock(height uint32) (*chain.Block, error) {
	var block chain.Block
	key := fmt.Sprintf("block:%d", height)

	err := bs.db.View(func(txn *badger.Txn) error {
		data, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		return data.Value(func(val []byte) error {
			return json.Unmarshal(val, &block)
		})
	})
	if err != nil {
		return nil, err
	}

	return &block, nil
}

func checkDBExists(dbPath string) bool {
	// Перевіряємо наявність MANIFEST файлу BadgerDB
	manifestPath := filepath.Join(dbPath, "MANIFEST")
	if _, err := os.Stat(manifestPath); err == nil {
		return true
	}

	// Або перевіряємо чи директорія не пуста
	entries, err := os.ReadDir(dbPath)
	if err != nil {
		return false
	}

	return len(entries) > 0
}
