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

// SelectNextProposer детерміністично вибирає proposer на основі хешу блоку та номеру раунду.
// Різні раунди при одній висоті дають різних proposer-ів, що дозволяє пропускати
// недоступних або несправних validat-ів без зміни стану ланцюжка.
func SelectNextProposer(blockHash []byte, validators []Validator, round uint32) (*Validator, error) {
	if len(validators) == 0 {
		return nil, errors.New("empty validator set")
	}

	var totalAmount int64
	for _, v := range validators {
		totalAmount += v.Amount
	}

	if totalAmount == 0 {
		return &validators[0], nil
	}

	// Мікс хешу блоку з номером раунду для отримання різного proposer-а в кожному раунді
	roundBytes := []byte{byte(round >> 24), byte(round >> 16), byte(round >> 8), byte(round)}
	seed := sha256.Sum256(append(blockHash, roundBytes...))
	hashInt := new(big.Int).SetBytes(seed[:])
	pick := new(big.Int).Mod(hashInt, big.NewInt(totalAmount))

	var cumulative int64
	for i := range validators {
		cumulative += validators[i].Amount
		if pick.Int64() < cumulative {
			return &validators[i], nil
		}
	}

	return &validators[0], nil
}
