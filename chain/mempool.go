package chain

import (
	"errors"
	"sync"

	"github.com/PQlite/crypto"
)

type Mempool struct {
	mu  sync.Mutex
	TXs []*crypto.Transaction
}

func (m *Mempool) Add(tx *crypto.Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, txFromMem := range m.TXs {
		if txFromMem.Signature == tx.Signature {
			return errors.New("tx is already exists")
		}
	}
	if !crypto.Verify(*tx) {
		return errors.New("signature is not valid")
	}
	m.TXs = append(m.TXs, tx)
	return nil
}

func (m *Mempool) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.TXs)
}
