package chain

type UnsignTransaction struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float32 `json:"amount"`
	Timestamp int64   `json:"timestamp"`
	Nonce     int     `json:"nonce"`
}

type Transaction struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Amount    float32 `json:"amount"`
	Timestamp int64   `json:"timestamp"`
	Nonce     int     `json:"nonce"`
	Signature []byte  `json:"signature"`
	PubKey    []byte  `json:"pubkey"`
}

type Wallet struct {
	Priv string `json:"priv"`
	Pub  string `json:"pub"`
}

func (t Transaction) GetUnsignTransaction() UnsignTransaction {
	return UnsignTransaction{
		From:      t.From,
		To:        t.To,
		Amount:    t.Amount,
		Timestamp: t.Timestamp,
		Nonce:     t.Nonce,
	}
}
