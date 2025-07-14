// Package database write and read blocks from database
package database

import (
	"bytes"
	"encoding/gob"
	"strconv"

	"github.com/PQlite/core/chain"
	"github.com/dgraph-io/badger/v4"
)

type BlockDB struct {
	db *badger.DB
}

func NewBlockDB(path string) (*BlockDB, error) {
	opts := badger.DefaultOptions(path)
	opts.Logger = nil // Прибрати спам у stdout

	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}

	return &BlockDB{db: db}, nil
}

func (bdb *BlockDB) SaveBlock(block *chain.Block) error {
	return bdb.db.Update(func(txn *badger.Txn) error {
		var buf bytes.Buffer
		enc := gob.NewEncoder(&buf)
		if err := enc.Encode(block); err != nil {
			return err
		}

		// Ключ по висоті
		key := []byte("block:" + strconv.FormatUint(block.Height, 10))
		if err := txn.Set(key, buf.Bytes()); err != nil {
			return err
		}

		// Оновити останню висоту
		return txn.Set([]byte("lastHeight"), []byte(strconv.FormatUint(block.Height, 10)))
	})
}

func (bdb *BlockDB) GetBlock(height uint64) (*chain.Block, error) {
	var block chain.Block

	err := bdb.db.View(func(txn *badger.Txn) error {
		key := []byte("block:" + strconv.FormatUint(height, 10))
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			dec := gob.NewDecoder(bytes.NewReader(val))
			return dec.Decode(&block)
		})
	})
	if err != nil {
		return nil, err
	}
	return &block, nil
}

func (bdb *BlockDB) GetLastHeight() (uint64, error) {
	var height uint64

	err := bdb.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("lastHeight"))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			h, err := strconv.ParseUint(string(val), 10, 64)
			if err != nil {
				return err
			}
			height = h
			return nil
		})
	})
	if err != nil {
		return 0, err
	}
	return height, nil
}

func (bdb *BlockDB) Close() error {
	return bdb.db.Close()
}
