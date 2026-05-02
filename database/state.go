package database

import (
	"encoding/json"

	"github.com/PQlite/core/chain"
	"github.com/dgraph-io/badger/v4"
	"github.com/rs/zerolog/log"
)

var walletPrefix = []byte("wallet")

func (bs *BlockStorage) GetWalletByAddress(addr []byte) (chain.Wallet, error) {
	var wallet chain.Wallet
	key := make([]byte, len(walletPrefix)+len(addr))
	copy(key, walletPrefix)
	copy(key[len(walletPrefix):], addr)

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

func (bs *BlockStorage) GetAllWallets() ([]chain.Wallet, error) {
	var wallets []chain.Wallet
	err := bs.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = walletPrefix
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek(walletPrefix); it.ValidForPrefix(walletPrefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var wallet chain.Wallet
				if err := json.Unmarshal(val, &wallet); err != nil {
					return err
				}
				wallets = append(wallets, wallet)
				return nil
			})
			if err != nil {
				return err
			}
		}
		log.Debug().Int("count", len(wallets)).Msg("GetAllWallets: знайдено гаманців")
		return nil
	})
	return wallets, err
}

func (bs *BlockStorage) UpdateBalance(wallet *chain.Wallet) error {
	key := make([]byte, len(walletPrefix)+len(wallet.Address))
	copy(key, walletPrefix)
	copy(key[len(walletPrefix):], wallet.Address)

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
