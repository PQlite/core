// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"bytes"
	"errors"
	"sync"

	"github.com/rs/zerolog/log"
)

type Mempool struct {
	mu  sync.Mutex
	TXs []*Transaction // NOTE: я маю список посилань на транзакції, а не посилання на список
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

func (m *Mempool) ClearMempool(txs []*Transaction) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, tx := range txs {
		for index, localTX := range m.TXs {
			if bytes.Equal(localTX.Signature, tx.Signature) {
				m.TXs = append(m.TXs[:index], m.TXs[index+1:]...) // видалити зі слайсу
				log.Info().Hex("sig", tx.Signature).Msg("видалино транзакцію з mempool")
			}
		}
	}
}
