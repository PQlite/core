package chain

import (
	"crypto/sha3"
	"fmt"
	"math/big"
)

type Validator struct {
	Address []byte
	Amount  float32
}

func SelectNextProposer(prevBlockHash []byte, validators []Validator) (*Validator, error) {
	if len(validators) < 1 {
		return &Validator{}, fmt.Errorf("кількість вілідаторів меньше 1")
	}
	seed := sha3.Sum224(prevBlockHash)

	totalStake := uint64(0)
	for _, v := range validators {
		totalStake += uint64(v.Amount)
	}

	// Конвертуємо seed в число
	seedBigInt := new(big.Int).SetBytes(seed[:])
	totalStakeBigInt := big.NewInt(int64(totalStake))
	position := new(big.Int).Mod(seedBigInt, totalStakeBigInt).Uint64()

	// Знаходимо валідатора
	cumulative := uint64(0)
	for i := range validators {
		cumulative += uint64(validators[i].Amount)
		if position < cumulative {
			return &validators[i], nil
		}
	}

	return &validators[len(validators)-1], nil
}
