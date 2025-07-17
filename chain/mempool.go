package chain

import (
	"bytes"
	"errors"
	"log"
	"sync"

	"github.com/PQlite/crypto"
	"github.com/polydawn/refmt/json"
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
	txForVerify, err := json.Marshal(tx.GetUnsignTransaction())
	if err != nil {
		log.Printf("Помилка під час серіалізації транзакції для перевірки: %v", err)
	}

	if !crypto.Verify(tx.PubKey, txForVerify, tx.Signature) {
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
