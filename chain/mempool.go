// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"bytes"
	"errors"
	"slices"
	"sync"
)

type Mempool struct {
	mu  sync.Mutex
	TXs []*Transaction
}

func (m *Mempool) Add(tx *Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.TXs) >= 1000 {
		return errors.New("OOM")
	}

	for _, txFromMem := range m.TXs {
		if bytes.Equal(txFromMem.Signature, tx.Signature) {
			return errors.New("tx is already exists")
		}
	}

	err := tx.Verify()
	if err != nil {
		return err
	}

	m.TXs = append(m.TXs, tx)

	return nil
}

func (m *Mempool) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.TXs)
}

// DeleteIfExist FIXME: не працює, просто не видаляє транзакції. треба зробити порівняння по signature
func (m *Mempool) DeleteIfExist(tx *Transaction) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if i := slices.Index(m.TXs, tx); i != -1 {
		m.TXs = slices.Delete(m.TXs, i, i+1)
		return true
	}
	return false
}
