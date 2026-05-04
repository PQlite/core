package chain

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math/big"
)

type Validator struct {
	Address []byte `json:"address"`
	Amount  int64  `json:"amount"`
}

// SelectNextProposer детерміністично вибирає proposer на основі хешу блоку та номеру раунду.
// Різні раунди при одній висоті дають різних proposer-ів, що дозволяє пропускати
// недоступних або несправних validat-ів без зміни стану ланцюжка.
// Параметр lastProposer дозволяє уникнути вибору того самого proposer-а два рази підряд.
func SelectNextProposer(blockHash []byte, validators []Validator, round uint32, lastProposer []byte) (*Validator, error) {
	if len(validators) == 0 {
		return nil, errors.New("empty validator set")
	}

	filteredValidators := validators
	if len(validators) > 1 && lastProposer != nil {
		filteredValidators = make([]Validator, 0, len(validators)-1)
		for _, v := range validators {
			if !bytes.Equal(v.Address, lastProposer) {
				filteredValidators = append(filteredValidators, v)
			}
		}
	}

	if len(filteredValidators) == 0 {
		filteredValidators = validators
	}

	var totalAmount int64
	for _, v := range filteredValidators {
		totalAmount += v.Amount
	}

	if totalAmount == 0 {
		return &filteredValidators[0], nil
	}

	// Мікс хешу блоку з номером раунду для отримання різного proposer-а в кожному раунді
	roundBytes := []byte{byte(round >> 24), byte(round >> 16), byte(round >> 8), byte(round)}
	seed := sha256.Sum256(append(blockHash, roundBytes...))
	hashInt := new(big.Int).SetBytes(seed[:])
	pick := new(big.Int).Mod(hashInt, big.NewInt(totalAmount))

	var cumulative int64
	for i := range filteredValidators {
		cumulative += filteredValidators[i].Amount
		if pick.Int64() < cumulative {
			return &filteredValidators[i], nil
		}
	}

	return &filteredValidators[0], nil
}
