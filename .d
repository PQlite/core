package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Використання: go run delete_blocks.go <шлях_до_BadgerDB> <макс_висота>")
		return
	}

	dbPath := os.Args[1]
	maxHeightStr := os.Args[2]

	maxHeight, err := strconv.ParseUint(maxHeightStr, 10, 64)
	if err != nil {
		fmt.Println("Помилка: невірне значення висоти")
		return
	}

	opts := badger.DefaultOptions(dbPath)
	db, err := badger.Open(opts)
	if err != nil {
		fmt.Println("Не вдалося відкрити базу даних:", err)
		return
	}
	defer db.Close()

	txn := db.NewTransaction(true)

	optsIter := badger.DefaultIteratorOptions
	optsIter.Prefix = []byte("block:")
	it := txn.NewIterator(optsIter)

	var deleted int
	for it.Rewind(); it.Valid(); it.Next() {
		item := it.Item()
		key := item.KeyCopy(nil)

		numStr := strings.TrimPrefix(string(key), "block:")
		blockHeight, err := strconv.ParseUint(numStr, 10, 64)
		if err != nil {
			continue
		}

		if blockHeight > maxHeight {
			if err := txn.Delete(key); err != nil {
				fmt.Println("Помилка видалення ключа", string(key), ":", err)
				continue
			}
			deleted++
		}
	}

	it.Close() // обов’язково закриваємо ітератор перед комітом
	if err := txn.Commit(); err != nil {
		fmt.Println("Помилка коміту транзакції:", err)
		return
	}

	fmt.Printf("Видалено %d блоків вище висоти %d\n", deleted, maxHeight)
}
