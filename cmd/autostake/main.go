package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/PQlite/core/chain"
)

type keyFile struct {
	Priv []byte `json:"priv"`
	Pub  []byte `json:"pub"`
}

func main() {
	keyPath := flag.String("key", ".env", "файл з ключами гаманця")
	node := flag.String("node", "http://localhost:8081", "адреса ноди")
	interval := flag.Duration("interval", 5*time.Second, "інтервал перевірки")
	flag.Parse()

	kf := loadKey(*keyPath)
	fmt.Printf("Autostake запущено для: %s\n", hex.EncodeToString(kf.Pub))

	ticker := time.NewTicker(*interval)
	for range ticker.C {
		wallet := fetchWallet(*node, kf.Pub)
		if wallet.Balance > 0 {
			fmt.Printf("Баланс: %s. Стейкаю все...\n", chain.FormatAmount(wallet.Balance))

			tx := chain.Transaction{
				From:      kf.Pub,
				To:        []byte("stake"),
				Amount:    wallet.Balance,
				Timestamp: time.Now().UnixMilli(),
				Nonce:     wallet.Nonce + 1,
			}

			if err := tx.Sign(kf.Priv); err != nil {
				fmt.Printf("Помилка підпису: %v\n", err)
				continue
			}

			data, _ := json.Marshal(tx)
			resp, err := http.Post(*node+"/tx", "application/json", bytes.NewReader(data))
			if err != nil {
				fmt.Printf("Помилка відправки tx: %v\n", err)
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			if resp.StatusCode == 200 {
				fmt.Println("Транзакцію stake надіслано!")
			} else {
				fmt.Printf("Помилка API: статус %d\n", resp.StatusCode)
			}
		}
	}
}

func loadKey(path string) keyFile {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	var kf keyFile
	json.Unmarshal(data, &kf)
	return kf
}

func fetchWallet(node string, pub []byte) chain.Wallet {
	url := fmt.Sprintf("%s/addr/%s", node, hex.EncodeToString(pub))
	resp, err := http.Get(url)
	if err != nil {
		return chain.Wallet{}
	}
	defer resp.Body.Close()
	var w chain.Wallet
	json.NewDecoder(resp.Body).Decode(&w)
	return w
}
