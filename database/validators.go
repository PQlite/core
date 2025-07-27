// Package database manages all interactions with the underlying key-value store (BadgerDB),
// handling the persistence of blocks, state, and validator data.
package database

import (
	"bytes"
	"encoding/binary"

	"github.com/PQlite/core/chain"
)

func (bs *BlockStorage) AddValidator(validator *chain.Validator) error {
	txn := bs.db.NewTransaction(true)
	defer txn.Discard()

	key := getValidatorKey(validator.Address)

	err := txn.Set(key, float32ToBytes(validator.Amount))
	if err != nil {
		return err
	}
	return txn.Commit()
}

func float32ToBytes(f float32) []byte {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, f)
	return buf.Bytes()
}

func bytesToFloat32(b []byte) float32 {
	var f float32
	_ = binary.Read(bytes.NewReader(b), binary.LittleEndian, &f)
	return f
}

// додає v_ до адреси
func getValidatorKey(a []byte) []byte {
	return append([]byte("v_"), a...)
}
