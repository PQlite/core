// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"bytes"
	"errors"
	"fmt"
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

	// NOTE: ця перевірка майже завжди буде давати true, тому що timestamp різні.
	// але це все одно захист від дублікатів
	for _, txFromMem := range m.TXs {
		if bytes.Equal(txFromMem.Signature, tx.Signature) {
			return errors.New("tx is already exists")
		}
	}

	isValid, err := tx.Verify()
	if err != nil {
		return err
	}
	if isValid {
		m.TXs = append(m.TXs, tx)
		return nil
	}
	return fmt.Errorf("signature is not valid")
}
