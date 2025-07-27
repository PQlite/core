package chain

type Validator struct {
	Address []byte
	Amount  float32
}

func SelectNextProposer(prevBlockHash []byte, validators *[]Validator) Validator {
	// TODO: написати логіку
	return Validator{}
}
