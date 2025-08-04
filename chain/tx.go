// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"encoding/json"
	"log"

	"github.com/PQlite/crypto"
)

type Transaction struct {
	From      []byte  `json:"from"`
	To        []byte  `json:"to"`
	Amount    float32 `json:"amount"`
	Timestamp int64   `json:"timestamp"`
	Nonce     int     `json:"nonce"`
	PubKey    []byte  `json:"pubkey"`
	Signature []byte  `json:"signature"`
}

// NOTE: воно взагалі треба?
type Wallet struct {
	Priv string `json:"priv"`
	Pub  string `json:"pub"`
}

func (t Transaction) GetUnsignTransaction() *Transaction {
	return &Transaction{
		From:      t.From,
		To:        t.To,
		Amount:    t.Amount,
		Timestamp: t.Timestamp,
		Nonce:     t.Nonce,
		PubKey:    t.PubKey,
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

func (t *Transaction) Verify() (bool, error) {
	unTx := t.GetUnsignTransaction()
	data, err := json.Marshal(unTx)
	if err != nil {
		log.Printf("Помилка під час серіалізації транзакції для перевірки: %v", err)
		return false, err
	}

	return crypto.Verify(t.PubKey, data, t.Signature)
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
