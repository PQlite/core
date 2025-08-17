package chain

type Wallet struct {
	Address []byte  `json:"address"`
	Balance float32 `json:"balance"`
	Nonce   uint32  `json:"nonce"`
}
