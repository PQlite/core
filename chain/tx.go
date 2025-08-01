// Package chain defines the core data structures and rules of the blockchain,
// including blocks, transactions, and consensus logic.
package chain

type Transaction struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float32 `json:"amount"`
	Timestamp int64   `json:"timestamp"`
	Nonce     int     `json:"nonce"`
	PubKey    []byte  `json:"pubkey"`
	Signature []byte  `json:"signature"`
}

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
