// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"sync"

	"github.com/PQlite/crypto"
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

	txForVerify, err := json.Marshal(tx.GetUnsignTransaction())
	if err != nil {
		log.Printf("Помилка під час серіалізації транзакції для перевірки: %v", err)
	}

	isValid, err := crypto.Verify(tx.PubKey, txForVerify, tx.Signature)
	if !isValid || err != nil {
		return errors.New("signature is not valid")
	}
	m.TXs = append(m.TXs, tx)
	return nil
}

// NOTE: це взагалі навіщо?
func (m *Mempool) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.TXs)
}
