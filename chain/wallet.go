package chain

type Wallet struct {
	Address []byte `json:"address"`
	Balance int64  `json:"balance"`
	Nonce   uint32 `json:"nonce"`
}
