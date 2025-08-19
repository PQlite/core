package chain

import (
	"crypto/sha256"
	"errors"
	"math/big"
)

type Validator struct {
	Address []byte
	Amount  int64
}

func SelectNextProposer(blockHash []byte, validators []Validator) (*Validator, error) {
	if len(validators) == 0 {
		return nil, errors.New("empty validator set")
	}

	var totalAmount int64
	for _, v := range validators {
		totalAmount += v.Amount
	}

	if totalAmount == 0 {
		// If total stake is 0, we can just pick the first validator
		return &validators[0], nil
	}

	// Use the block hash to deterministically select a proposer
	seed := sha256.Sum256(blockHash)
	hashInt := new(big.Int).SetBytes(seed[:])
	pick := new(big.Int).Mod(hashInt, big.NewInt(totalAmount))

	var cumulativeAmount int64
	for i := range validators {
		cumulativeAmount += validators[i].Amount
		if pick.Int64() < cumulativeAmount {
			return &validators[i], nil
		}
	}

	// This part should not be reached if logic is correct, but as a fallback
	return &validators[0], nil
}
