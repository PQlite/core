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

const (
	defaultNode    = "http://localhost:8081"
	defaultKeyFile = ".env"
)

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
	amountFloat := flag.Float64("amount", 1, "сума кожної транзакції")
	timeout := flag.Duration("timeout", 3*time.Second, "таймаут очікування підтвердження блоків")
	flag.Parse()

	amount := int64(*amountFloat * float64(chain.Precision))

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
	fmt.Printf("Транзакцій: %d (по %s PQL), паралельно: %d\n\n", *count, chain.FormatAmount(amount), *workers)

	// Поточний nonce
	initialNonce := fetchNonce(*node, kf.Pub)
	nonceCounter := initialNonce
	fmt.Printf("Поточний nonce: %d → починаємо з %d\n\n", initialNonce-1, initialNonce)

	// Фаза 1: відправка
	fmt.Printf("Відправляю %d транзакцій...\n", *count)
	submitStart := time.Now()

	var sent atomic.Int64
	var errors atomic.Int64
	var lastSentNonce atomic.Uint32
	lastSentNonce.Store(initialNonce - 1)

	workerDone := make(chan struct{}, *workers)
	
	// Розподіляємо кількість задач між воркерами
	tasksPerWorker := *count / *workers
	extraTasks := *count % *workers

	for w := 0; w < *workers; w++ {
		numTasks := tasksPerWorker
		if w < extraTasks {
			numTasks++
		}
		
		go func(nTasks int) {
			client := &http.Client{Timeout: 5 * time.Second}
			for i := 0; i < nTasks; i++ {
				// Отримуємо унікальний nonce
				currentNonce := atomic.AddUint32(&nonceCounter, 1) - 1
				
				tx := chain.Transaction{
					From:      kf.Pub,
					To:        toBytes,
					Amount:    amount,
					Timestamp: time.Now().UnixMilli(),
					Nonce:     currentNonce,
				}
				
				if err := tx.Sign(kf.Priv); err != nil {
					errors.Add(1)
					continue
				}
				
				data, _ := json.Marshal(tx)
				
				resp, err := client.Post(*node+"/tx", "application/json", bytes.NewReader(data))
				if err != nil {
					errors.Add(1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				
				if resp.StatusCode == 200 {
					sent.Add(1)
					// Оновлюємо максимально відправлений nonce (приблизно)
					for {
						old := lastSentNonce.Load()
						if currentNonce <= old {
							break
						}
						if lastSentNonce.CompareAndSwap(old, currentNonce) {
							break
						}
					}
				} else {
					errors.Add(1)
				}
			}
			workerDone <- struct{}{}
		}(numTasks)
	}

	for w := 0; w < *workers; w++ {
		<-workerDone
	}

	submitDur := time.Since(submitStart)
	sentN := int(sent.Load())
	errN := int(errors.Load())
	maxNonce := lastSentNonce.Load()

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

	lastHeight := uint32(0)
	confirmedByBlock := make(map[uint32]int)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var totalConfirmed int
	var firstBlockTime time.Duration
	
	// Карта для відстеження підтверджених nonce
	confirmedNonces := make(map[uint32]bool)

	for time.Now().Before(deadline) {
		<-ticker.C

		blocks := fetchBlocks(*node)
		for _, b := range blocks {
			if b.Height <= lastHeight {
				continue
			}

			newConfirmations := 0
			for _, tx := range b.Transactions {
				if bytes.Equal(tx.From, kf.Pub) && tx.Nonce >= initialNonce && tx.Nonce <= maxNonce {
					if !confirmedNonces[tx.Nonce] {
						confirmedNonces[tx.Nonce] = true
						newConfirmations++
					}
				}
			}

			if newConfirmations > 0 {
				elapsed := time.Since(submitStart)
				if firstBlockTime == 0 {
					firstBlockTime = elapsed
				}
				confirmedByBlock[b.Height] += newConfirmations
				totalConfirmed += newConfirmations
				ts := time.UnixMilli(b.Timestamp).Format("15:04:05.000")
				fmt.Printf("  Блок #%-4d [%s] — %d нових txs підтверджено (всього %d/%d, +%.1fs від старту)\n",
					b.Height, ts, newConfirmations, totalConfirmed, sentN, elapsed.Seconds())
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
