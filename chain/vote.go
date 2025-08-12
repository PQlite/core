package chain

import (
	"github.com/PQlite/crypto"
)

type VoteCh chan Vote

type Vote struct {
	Pub       []byte `json:"pub"`
	Signature []byte `json:"signature"`
}

func (v *Vote) Verify(b *Block) error {
	blockBytes, err := b.MarshalDeterministic()
	if err != nil {
		return err
	}

	if err = crypto.Verify(v.Pub, blockBytes, v.Signature); err != nil {
		return err
	}
	return nil
}
