package p2p

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"

	"github.com/PQlite/crypto"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/rs/zerolog/log"
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

func LoadKeys() (*Keys, error) {
	_, err := os.Stat(filePath)
	if err == nil {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		var keys Keys
		err = json.Unmarshal(data, &keys)
		if err != nil {
			return nil, err
		}
		return &keys, nil
	} else {
		log.Info().Msg("не знайдено ключів, створюю нові")
		pub, priv, err := crypto.Create()
		if err != nil {
			return nil, err
		}

		err = save(priv, pub)
		if err != nil {
			return nil, err
		}
		return &Keys{Priv: priv, Pub: pub}, nil
	}
}

func LoadOrCreateIdentity(path string) (libp2pcrypto.PrivKey, error) {
	// HACK: це написав ChatGPT
	// Перевіряємо, чи вже існує ключ
	if _, err := os.Stat(path); err == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}

		privKey, err := libp2pcrypto.UnmarshalPrivateKey(data)
		if err != nil {
			return nil, errors.New("не вдалося розпакувати приватний ключ: " + err.Error())
		}

		return privKey, nil
	}

	// Створюємо новий ключ
	privKey, _, err := libp2pcrypto.GenerateKeyPairWithReader(libp2pcrypto.Ed25519, -1, rand.Reader)
	if err != nil {
		return nil, err
	}

	// Серіалізуємо
	bytes, err := libp2pcrypto.MarshalPrivateKey(privKey)
	if err != nil {
		return nil, err
	}

	// Зберігаємо у файл
	err = os.WriteFile(path, bytes, 0600)
	if err != nil {
		return nil, err
	}

	return privKey, nil
}
