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

func (m *Mempool) GetTransactions() []*Transaction {
	m.mu.Lock()
	defer m.mu.Unlock()

	txs := make([]*Transaction, len(m.TXs))
	copy(txs, m.TXs)
	return txs
}

func (m *Mempool) SetTransactions(txs []*Transaction) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TXs = txs
}

func (m *Mempool) ClearMempool(txs []*Transaction) {
	m.mu.Lock()
	defer m.mu.Unlock()

	removeMap := make(map[string]bool)
	for _, tx := range txs {
		removeMap[string(tx.Signature)] = true
	}

	newTXs := make([]*Transaction, 0, len(m.TXs))
	for _, localTX := range m.TXs {
		if !removeMap[string(localTX.Signature)] {
			newTXs = append(newTXs, localTX)
		} else {
			log.Info().Hex("sig", localTX.Signature).Msg("видалино транзакцію з mempool")
		}
	}
	m.TXs = newTXs
}
