// Package database manages all interactions with the underlying key-value store (BadgerDB),
// handling the persistence of blocks, state, and validator data.
package database

import (
	"encoding/binary"

	"github.com/PQlite/core/chain"
	"github.com/dgraph-io/badger/v4"
)

func (bs *BlockStorage) AddValidator(validator *chain.Validator) error {
	txn := bs.db.NewTransaction(true)
	defer txn.Discard()

	key := getValidatorKey(validator.Address)

	err := txn.Set(key, int64ToBytes(validator.Amount))
	if err != nil {
		return err
	}
	return txn.Commit()
}

func (bs *BlockStorage) GetValidatorsList() (*[]chain.Validator, error) {
	var res []chain.Validator

	err := bs.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.Prefix = []byte("v_")

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			err := item.Value(func(v []byte) error {
				address := getValidatorAddress(key)
				amount := bytesToInt64(v)
				res = append(res, chain.Validator{Address: address, Amount: amount})
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return &res, err
}

func int64ToBytes(i int64) []byte {
	iBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(iBytes, uint64(i))
	return iBytes
}

func bytesToInt64(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(b))
}

// додає v_ до адреси
func getValidatorKey(a []byte) []byte {
	return append([]byte("v_"), a...)
}

func getValidatorAddress(a []byte) []byte {
	return a[len([]byte("v_")):]
}
