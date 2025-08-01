package p2p

import (
	"encoding/json"
	"log"
	"os"

	"github.com/PQlite/crypto"
)

type Keys struct {
	Priv []byte `json:"priv"`
	Pub  []byte `json:"pub"`
}

const (
	filePath = ".env"
)

// NOTE: я швидко писав, тому може бути не дуже

func save(priv []byte, pub []byte) error {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := json.Marshal(Keys{
		Priv: priv,
		Pub:  pub,
	})
	if err != nil {
		return err
	}

	// TODO: зробити шифрування
	_, err = file.Write(data)
	return err
}

func loadKeys() (*Keys, error) {
	_, err := os.Stat(filePath)
	if err == nil {
		data, err := os.ReadFile(filePath)
		if err != nil {
			log.Println("", err)
			return nil, err
		}
		var keys Keys
		err = json.Unmarshal(data, &keys)
		if err != nil {
			log.Println("", err)
			return nil, err
		}
		return &keys, nil
	} else {
		pub, priv, err := crypto.Create()
		if err != nil {
			log.Println(err)
			return nil, err
		}
		binPriv, err := priv.MarshalBinary()
		if err != nil {
			log.Println("", err)
			return nil, err
		}
		binPub, err := pub.MarshalBinary()
		if err != nil {
			log.Println("", err)
			return nil, err
		}
		err = save(binPriv, binPub)
		if err != nil {
			log.Println("", err)
			return nil, err
		}
		return &Keys{Priv: binPriv, Pub: binPub}, nil
	}
}
