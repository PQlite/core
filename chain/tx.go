// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/PQlite/crypto"
	"github.com/rs/zerolog/log"
)

type Transaction struct {
	From      []byte `json:"from"`
	To        []byte `json:"to"`
	Amount    int64  `json:"amount"`
	Fee       int64  `json:"fee"`
	Timestamp int64  `json:"timestamp"`
	Nonce     uint32 `json:"nonce"`
	Signature []byte `json:"signature"`
}

func (t Transaction) GetUnsignTransaction() *Transaction {
	return &Transaction{
		From:      t.From,
		To:        t.To,
		Amount:    t.Amount,
		Fee:       t.Fee,
		Timestamp: t.Timestamp,
		Nonce:     t.Nonce,
	}
}

func (t *Transaction) Sign(priv []byte) error {
	unTx := t.GetUnsignTransaction()
	data, err := json.Marshal(unTx)
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

	// вийнятки для системних адрес.
	var pubKey []byte
	if bytes.Equal(t.From, []byte("reward")) || bytes.Equal(t.From, []byte("stake")) {
		pubKey = t.To
	} else if bytes.Equal(t.To, []byte("fine")) {
		// Штрафні транзакції не перевіряються за підписом відправника тут,
		// бо їх створює proposer блоку. Валідація відбувається на рівні блоку.
		return nil
	} else {
		pubKey = t.From
	}

	if err = crypto.Verify(pubKey, data, t.Signature); err != nil {
		return err
	}
	return nil
}

// FormatAmount converts an internal int64 amount to a human-readable decimal string.
func FormatAmount(amount int64) string {
	major := amount / Precision
	minor := amount % Precision
	if minor < 0 {
		minor = -minor
	}
	// Assuming Precision is 100, we want 2 decimal places.
	// If Precision changes, this formatting logic might need adjustment.
	return fmt.Sprintf("%d.%02d", major, minor)
}

// ParseAmount converts a decimal string to the internal int64 representation.
func ParseAmount(s string) (int64, error) {
	// A simple implementation, might need better error handling
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(f * float64(Precision)), nil
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
