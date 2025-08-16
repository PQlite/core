package chain

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
)

type Validator struct {
	Address []byte
	Amount  float32
}

// NOTE: можливо треба додати отримання останнього блоку, і списку валідаторів, тому що вони не завжди під рукою
// SelectNextProposer deterministically selects the next proposer based on block hash
func SelectNextProposer(blockHash []byte, validators []Validator) (*Validator, error) {
	if len(validators) == 0 {
		return nil, errors.New("empty validators list")
	}

	// Calculate total stake
	totalStake := float32(0)
	for _, v := range validators {
		if v.Amount < 0 {
			return nil, errors.New("negative stake amount")
		}
		totalStake += v.Amount
	}

	if totalStake == 0 {
		return nil, errors.New("total stake is zero")
	}

	// Use block hash as seed
	hash := sha256.Sum256(blockHash)
	seed := binary.BigEndian.Uint64(hash[:8]) // Use first 8 bytes as seed

	// Generate deterministic random value between 0 and 1
	randVal := float32(seed) / float32(math.MaxUint64)

	// Select validator based on weighted random selection
	target := randVal * totalStake
	current := float32(0)

	for _, v := range validators {
		current += v.Amount
		if current >= target {
			return &v, nil
		}
	}

	// Fallback to last validator in case of rounding errors
	return &validators[len(validators)-1], nil
}

// func SelectNextProposer(prevBlockHash []byte, validators []Validator) (*Validator, error) {
// 	// FIXME: не може працювати з числами float
// 	if len(validators) < 1 {
// 		return &Validator{}, fmt.Errorf("кількість вілідаторів меньше 1")
// 	}
// 	seed := sha3.Sum224(prevBlockHash)
//
// 	totalStake := uint64(0)
// 	for _, v := range validators {
// 		totalStake += uint64(v.Amount)
// 	}
//
// 	// Конвертуємо seed в число
// 	seedBigInt := new(big.Int).SetBytes(seed[:])
// 	totalStakeBigInt := big.NewInt(int64(totalStake))
// 	position := new(big.Int).Mod(seedBigInt, totalStakeBigInt).Uint64()
//
// 	// Знаходимо валідатора
// 	cumulative := uint64(0)
// 	for i := range validators {
// 		cumulative += uint64(validators[i].Amount)
// 		if position < cumulative {
// 			return &validators[i], nil
// 		}
// 	}
//
// 	return &validators[len(validators)-1], nil
// }
