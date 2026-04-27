// Тест пропускної здатності PQlite.
// Відправляє N транзакцій і вимірює: швидкість прийому API, час виробництва блоків, ефективний TPS.
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
	"sync/atomic"
	"time"

	"github.com/PQlite/core/chain"
)

const defaultNode = "http://localhost:8081"
const defaultKeyFile = ".env"

type keyFile struct {
	Priv []byte `json:"priv"`
	Pub  []byte `json:"pub"`
}

func main() {
	keyPath := flag.String("key", defaultKeyFile, "файл з ключами відправника")
	toHex := flag.String("to", "", "адреса отримувача hex (за замовчуванням — сам собі)")
	count := flag.Int("count", 100, "кількість транзакцій")
	workers := flag.Int("par", 4, "кількість паралельних воркерів відправки")
	node := flag.String("node", defaultNode, "адреса ноди")
	amount := flag.Int64("amount", 1, "сума кожної транзакції")
	timeout := flag.Duration("timeout", 60*time.Second, "таймаут очікування підтвердження блоків")
	flag.Parse()

	kf := loadKey(*keyPath)

	var toBytes []byte
	if *toHex == "" {
		toBytes = kf.Pub
	} else {
		var err error
		toBytes, err = hex.DecodeString(*toHex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "невірна hex адреса: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("=== PQlite Throughput Bench ===\n")
	fmt.Printf("Від:      %s\n", hex.EncodeToString(kf.Pub))
	fmt.Printf("Кому:     %s\n", hex.EncodeToString(toBytes))
	fmt.Printf("Транзакцій: %d (по %d), паралельно: %d\n\n", *count, *amount, *workers)

	// Поточний nonce
	startNonce := fetchNonce(*node, kf.Pub)
	fmt.Printf("Поточний nonce: %d → починаємо з %d\n\n", startNonce-1, startNonce)

	// Будуємо всі транзакції заздалегідь (nonce строго послідовний)
	txs := make([][]byte, *count)
	for i := 0; i < *count; i++ {
		tx := chain.Transaction{
			From:      kf.Pub,
			To:        toBytes,
			Amount:    *amount,
			Timestamp: time.Now().UnixMilli(),
			Nonce:     startNonce + uint32(i),
		}
		if err := tx.Sign(kf.Priv); err != nil {
			fmt.Fprintf(os.Stderr, "помилка підпису tx %d: %v\n", i, err)
			os.Exit(1)
		}
		data, err := json.Marshal(tx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "помилка серіалізації tx %d: %v\n", i, err)
			os.Exit(1)
		}
		txs[i] = data
	}

	// Фаза 1: відправка
	fmt.Printf("Відправляю %d транзакцій...\n", *count)
	submitStart := time.Now()

	jobs := make(chan []byte, *count)
	for _, tx := range txs {
		jobs <- tx
	}
	close(jobs)

	var sent atomic.Int64
	var errors atomic.Int64

	workerDone := make(chan struct{}, *workers)
	for w := 0; w < *workers; w++ {
		go func() {
			client := &http.Client{Timeout: 5 * time.Second}
			for tx := range jobs {
				resp, err := client.Post(*node+"/tx", "application/json", bytes.NewReader(tx))
				if err != nil {
					errors.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					sent.Add(1)
				} else {
					errors.Add(1)
				}
			}
			workerDone <- struct{}{}
		}()
	}

	for w := 0; w < *workers; w++ {
		<-workerDone
	}

	submitDur := time.Since(submitStart)
	sentN := int(sent.Load())
	errN := int(errors.Load())

	fmt.Printf("Відправлено: %d/%d (помилок: %d) за %.2fs → %.1f tx/s (до API)\n\n",
		sentN, *count, errN, submitDur.Seconds(),
		float64(sentN)/submitDur.Seconds())

	if sentN == 0 {
		fmt.Println("Жодна транзакція не була прийнята. Зупиняюсь.")
		os.Exit(1)
	}

	// Фаза 2: очікування підтвердження в блоках
	fmt.Printf("Очікую підтвердження в блоках (таймаут: %s)...\n", *timeout)
	waitStart := time.Now()
	deadline := waitStart.Add(*timeout)

	targetNonce := startNonce + uint32(sentN) - 1
	lastHeight := uint32(0)
	confirmedByBlock := make(map[uint32]int)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var totalConfirmed int
	var firstBlockTime time.Duration

	for time.Now().Before(deadline) {
		<-ticker.C

		blocks := fetchBlocks(*node)
		for _, b := range blocks {
			if b.Height <= lastHeight {
				continue
			}

			txCount := 0
			for _, tx := range b.Transactions {
				if bytes.Equal(tx.From, kf.Pub) && tx.Nonce >= startNonce && tx.Nonce <= targetNonce {
					txCount++
				}
			}

			if txCount > 0 {
				elapsed := time.Since(submitStart)
				if firstBlockTime == 0 {
					firstBlockTime = elapsed
				}
				confirmedByBlock[b.Height] = txCount
				totalConfirmed += txCount
				ts := time.UnixMilli(b.Timestamp).Format("15:04:05.000")
				fmt.Printf("  Блок #%-4d [%s] — %d txs підтверджено (+%.1fs від старту)\n",
					b.Height, ts, txCount, elapsed.Seconds())
				lastHeight = b.Height
			}
		}

		if totalConfirmed >= sentN {
			break
		}
	}

	waitDur := time.Since(waitStart)

	// Фаза 3: звіт
	fmt.Printf("\n=== Результати ===\n")
	fmt.Printf("Відправлено до API:      %d txs за %.2fs (%.1f tx/s)\n",
		sentN, submitDur.Seconds(), float64(sentN)/submitDur.Seconds())
	fmt.Printf("Підтверджено в блоках:   %d/%d\n", totalConfirmed, sentN)

	if totalConfirmed > 0 {
		totalWait := submitDur + waitDur
		fmt.Printf("Час до першого блоку:   %.2fs\n", firstBlockTime.Seconds())
		fmt.Printf("Загальний час:           %.2fs\n", totalWait.Seconds())
		fmt.Printf("Ефективний TPS:          %.1f tx/s\n", float64(totalConfirmed)/totalWait.Seconds())

		if len(confirmedByBlock) > 1 {
			heights := make([]uint32, 0, len(confirmedByBlock))
			for h := range confirmedByBlock {
				heights = append(heights, h)
			}
			// Середній час блоку по кількості блоків
			fmt.Printf("Блоків з нашими txs:    %d\n", len(confirmedByBlock))
		}
	} else {
		fmt.Printf("Жодна транзакція не підтверджена за %s\n", *timeout)
	}
}

func loadKey(path string) keyFile {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "не вдалось прочитати файл ключів %s: %v\n", path, err)
		os.Exit(1)
	}
	var kf keyFile
	if err = json.Unmarshal(data, &kf); err != nil {
		// Спробуємо формат з crypto.Create (pub, priv окремо)
		fmt.Fprintf(os.Stderr, "невірний формат файлу ключів: %v\n", err)
		os.Exit(1)
	}
	return kf
}

func fetchNonce(node string, pub []byte) uint32 {
	url := fmt.Sprintf("%s/addr/%s", node, hex.EncodeToString(pub))
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "помилка отримання nonce: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var wallet chain.Wallet
	if err = json.Unmarshal(body, &wallet); err != nil {
		fmt.Fprintf(os.Stderr, "помилка читання wallet: %v\n", err)
		os.Exit(1)
	}
	return wallet.Nonce + 1
}

func fetchBlocks(node string) []chain.Block {
	resp, err := http.Get(node + "/blocks")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var blocks []chain.Block
	json.Unmarshal(body, &blocks)
	return blocks
}

