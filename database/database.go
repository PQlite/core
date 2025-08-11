// Package database тут усе для роботи з базою даних
// NOTE: треба зрозуміти, як працювати з таблицями в цій базі
package database

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/PQlite/core/chain"
	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
)

type BlockStorage struct {
	db *badger.DB
}

func InitDB() (*BlockStorage, error) {
	opts := badger.DefaultOptions("/tmp/badger")
	opts.Compression = options.Snappy
	db, err := badger.Open(opts)
	bs := &BlockStorage{db: db}
	if err != nil {
		return nil, err
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

func (bs *BlockStorage) GetLastBlock() (*chain.Block, error) {
	var lastBlock *chain.Block
	var maxBlockNumber int64 = -1

	err := bs.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("block:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()
			keyStr := string(key)

			numStr := strings.TrimPrefix(keyStr, "block:")
			blockNumber, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil {
				continue
			}

			if blockNumber > maxBlockNumber {
				maxBlockNumber = blockNumber
			}
		}

		if maxBlockNumber != -1 {
			keyToFetch := []byte("block:" + strconv.FormatInt(maxBlockNumber, 10))
			item, err := txn.Get(keyToFetch)
			if err != nil {
				return err
			}

			return item.Value(func(val []byte) error {
				var block chain.Block
				if err := json.Unmarshal(val, &block); err != nil {
					return err
				}
				lastBlock = &block
				return nil
			})
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if lastBlock == nil {
		return nil, fmt.Errorf("no blocks found")
	}

	return lastBlock, nil
}

func (bs *BlockStorage) GetAllBlocks() ([]*chain.Block, error) {
	var blocks []*chain.Block

	err := bs.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("block:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var block chain.Block
				if err := json.Unmarshal(val, &block); err != nil {
					return err
				}
				blocks = append(blocks, &block)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return blocks, nil
}
