// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"encoding/json"

	"github.com/PQlite/crypto"
	"github.com/rs/zerolog/log"
)

type Transaction struct {
	From      []byte  `json:"from"`
	To        []byte  `json:"to"`
	Amount    float32 `json:"amount"`
	Timestamp int64   `json:"timestamp"`
	Nonce     uint32  `json:"nonce"`
	Signature []byte  `json:"signature"`
}

func (t Transaction) GetUnsignTransaction() *Transaction {
	return &Transaction{
		From:      t.From,
		To:        t.To,
		Amount:    t.Amount,
		Timestamp: t.Timestamp,
		Nonce:     t.Nonce,
	}
}

func (t *Transaction) Sign(priv []byte) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}

	sign, err := crypto.Sign(priv, data)
	if err != nil {
		return err
	}

	t.Signature = sign
	return nil
}

// Verify якщо все ок, і транзакція пройшла перевірку, буде повернуто nil, в іншому випадку err з описом
func (t *Transaction) Verify() error {
	unTx := t.GetUnsignTransaction()
	data, err := json.Marshal(unTx)
	if err != nil {
		log.Error().Err(err).Msg("Помилка під час серіалізації транзакції для перевірки")
		return err
	}

	if err = crypto.Verify(t.From, data, t.Signature); err != nil {
		return err
	}
	return nil
}

// func VerifyAndAddValidators(t []*Transaction) error {
// 	for _, tx := range t {
// 		isValid, err := tx.Verify()
// 		if err != nil || !isValid {
// 			return fmt.Errorf("not valid")
// 		}
//
// 		if bytes.Equal(tx.To, []byte("stake")) {
//
// 		}
// 	}
// }
