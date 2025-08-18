package database

import (
	"encoding/json"

	"github.com/PQlite/core/chain"
	"github.com/dgraph-io/badger/v4"
)

var walletPrefix = []byte("wallet")

func (bs *BlockStorage) GetWalletByAddress(addr []byte) (chain.Wallet, error) {
	// TODO: треба щоб повертала структура wallet, якщо є помилка "Key not found"
	var wallet chain.Wallet
	key := append(walletPrefix, addr...)

	if err := bs.db.View(func(txn *badger.Txn) error {
		data, err := txn.Get(key)
		if err != nil {
			return err
		}

		return data.Value(func(val []byte) error {
			return json.Unmarshal(val, &wallet)
		})
	}); err != nil {
		if err.Error() == "Key not found" {
			return chain.Wallet{
				Address: addr,
				Balance: 0,
				Nonce:   0,
			}, nil
		}
		return wallet, err
	}

	return wallet, nil
}

func (bs *BlockStorage) UpdateBalance(wallet *chain.Wallet) error {
	key := append(walletPrefix, wallet.Address...)
	data, err := json.Marshal(wallet)
	if err != nil {
		return err
	}

	if err := bs.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, data)
	}); err != nil {
		return err
	}

	return bs.db.Sync()
}
