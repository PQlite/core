// Тест пропускної здатності PQlite.
// Відправляє N транзакцій і вимірює: швидкість прийому API, час виробництва блоків, ефективний TPS.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/crypto"
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
	keyPath := flag.String("key", defaultKeyFile, "файл з ключами відправника (головний гаманець)")
	toHex := flag.String("to", "", "адреса отримувача hex (якщо пуста — випадкові адреси)")
	count := flag.Int("count", 100, "кількість транзакцій")
	workers := flag.Int("par", 4, "кількість паралельних воркерів (гаманців)")
	node := flag.String("node", defaultNode, "адреса ноди")
	amountFloat := flag.Float64("amount", 0.1, "сума кожної транзакції")
	feeFloat := flag.Float64("fee", 0.01, "комісія")
	timeout := flag.Duration("timeout", 60*time.Second, "таймаут очікування підтвердження блоків")
	flag.Parse()

	amount := int64(*amountFloat * float64(chain.Precision))
	fee := int64(*feeFloat * float64(chain.Precision))

	mainKF := loadKey(*keyPath)
	
	fmt.Printf("=== PQlite Throughput Bench (Multi-Wallet) ===\n")
	fmt.Printf("Головний гаманець: %s\n", hex.EncodeToString(mainKF.Pub))
	fmt.Printf("Параметри: %d транзакцій, %d воркерів, сума: %s, комісія: %s\n\n", 
		*count, *workers, chain.FormatAmount(amount), chain.FormatAmount(fee))

	// Фаза 0: Підготовка та фінансування воркерів
	fmt.Printf("--- Фаза 0: Фінансування воркерів ---\n")
	workerKeys := make([]keyFile, *workers)
	tasksPerWorker := *count / *workers
	
	mainNonce := fetchNonce(*node, mainKF.Pub)
	client := &http.Client{Timeout: 10 * time.Second}

	for i := 0; i < *workers; i++ {
		pub, priv, _ := crypto.Create()
		workerKeys[i] = keyFile{Pub: pub, Priv: priv}
		
		numTasks := tasksPerWorker
		if i < *count%*workers {
			numTasks++
		}
		
		// Сума для воркера: (сума + комісія) * кількість задач
		fundAmount := int64(numTasks) * (amount + fee)
		
		tx := chain.Transaction{
			From:      mainKF.Pub,
			To:        pub,
			Amount:    fundAmount,
			Fee:       fee,
			Timestamp: time.Now().UnixMilli(),
			Nonce:     mainNonce,
		}
		mainNonce++
		
		fatal(tx.Sign(mainKF.Priv), "помилка підпису tx фінансування")
		data, _ := json.Marshal(tx)
		
		resp, err := client.Post(*node+"/tx", "application/json", bytes.NewReader(data))
		fatal(err, "помилка відправки tx фінансування")
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		
		if resp.StatusCode != 200 {
			fmt.Printf("Помилка фінансування воркера %d: статус %d\n", i, resp.StatusCode)
			os.Exit(1)
		}
	}
	fmt.Printf("Транзакції фінансування відправлені. Очікую підтвердження...\n")
	
	// Чекаємо поки останній воркер отримає кошти
	for {
		w := fetchWallet(*node, workerKeys[*workers-1].Pub)
		if w.Balance > 0 {
			fmt.Printf("Фінансування підтверджено!\n\n")
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Фаза 1: Відправка транзакцій воркерами
	fmt.Printf("--- Фаза 1: Відправка %d транзакцій ---\n", *count)
	submitStart := time.Now()

	var sent atomic.Int64
	var errors atomic.Int64
	workerDone := make(chan struct{}, *workers)

	var sentHashes []string
	var hashMu sync.Mutex

	for w := 0; w < *workers; w++ {
		numTasks := tasksPerWorker
		if w < *count%*workers {
			numTasks++
		}
		
		go func(workerID int, nTasks int) {
			wk := workerKeys[workerID]
			// Кожен воркер має свій nonce, починаємо з 1 (після фінансування)
			nonce := uint32(1) 
			
			for i := 0; i < nTasks; i++ {
				var target []byte
				if *toHex == "" {
					target = make([]byte, 32)
					rand.Read(target)
				} else {
					target, _ = hex.DecodeString(*toHex)
				}
				
				tx := chain.Transaction{
					From:      wk.Pub,
					To:        target,
					Amount:    amount,
					Fee:       fee,
					Timestamp: time.Now().UnixMilli(),
					Nonce:     nonce,
				}
				nonce++
				
				tx.Sign(wk.Priv)
				data, _ := json.Marshal(tx)
				
				// Відправляємо послідовно для кожного воркера, щоб зберегти порядок Nonce
				resp, err := client.Post(*node+"/tx", "application/json", bytes.NewReader(data))
				if err == nil && resp.StatusCode == 200 {
					sent.Add(1)
					var res struct { Hash string `json:"hash"` }
					if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && res.Hash != "" {
						hashMu.Lock()
						sentHashes = append(sentHashes, res.Hash)
						hashMu.Unlock()
					}
				} else {
					errors.Add(1)
				}
				if resp != nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}
			workerDone <- struct{}{}
		}(w, numTasks)
	}

	for w := 0; w < *workers; w++ {
		<-workerDone
	}

	submitDur := time.Since(submitStart)
	fmt.Printf("Відправлено: %d/%d (помилок: %d) за %.2fs (%.1f tx/s)\n\n",
		sent.Load(), *count, errors.Load(), submitDur.Seconds(),
		float64(sent.Load())/submitDur.Seconds())

	// Фаза 2: Очікування підтвердження
	fmt.Printf("--- Фаза 2: Очікування підтвердження в блоках ---\n")
	waitStart := time.Now()
	deadline := waitStart.Add(*timeout)
	
	confirmedCount := int64(0)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	confirmedMap := make(map[string]bool)

	for time.Now().Before(deadline) && confirmedCount < int64(len(sentHashes)) {
		<-ticker.C
		
		for _, h := range sentHashes {
			if confirmedMap[h] {
				continue
			}
			
			resp, err := http.Get(*node + "/tx/" + h)
			if err != nil {
				continue
			}
			
			var res struct { Status string `json:"status"` }
			if err := json.NewDecoder(resp.Body).Decode(&res); err == nil {
				if res.Status == "confirmed" {
					confirmedMap[h] = true
					confirmedCount++
				}
			}
			resp.Body.Close()
		}
		
		if confirmedCount > 0 {
			fmt.Printf("  Підтверджено: %d/%d (%.1fs від старту)\n", 
				confirmedCount, sent.Load(), time.Since(submitStart).Seconds())
		}
	}

	totalDur := time.Since(submitStart)
	fmt.Printf("\n=== Результати ===\n")
	fmt.Printf("Ефективний TPS: %.1f tx/s\n", float64(confirmedCount)/totalDur.Seconds())
	fmt.Printf("Загальний час:  %.2fs\n", totalDur.Seconds())
}

// --- Helpers ---

func loadKey(path string) keyFile {
	data, err := os.ReadFile(path)
	fatal(err, "не вдалось прочитати файл ключів")
	var kf keyFile
	fatal(json.Unmarshal(data, &kf), "невірний формат ключа")
	return kf
}

func fetchWallet(node string, pub []byte) chain.Wallet {
	url := fmt.Sprintf("%s/addr/%s", node, hex.EncodeToString(pub))
	resp, err := http.Get(url)
	if err != nil { return chain.Wallet{} }
	defer resp.Body.Close()
	var w chain.Wallet
	json.NewDecoder(resp.Body).Decode(&w)
	return w
}

func fetchNonce(node string, pub []byte) uint32 {
	return fetchWallet(node, pub).Nonce + 1
}

func fetchBlocks(node string) []chain.Block {
	resp, err := http.Get(node + "/blocks")
	if err != nil { return nil }
	defer resp.Body.Close()
	var blocks []chain.Block
	json.NewDecoder(resp.Body).Decode(&blocks)
	return blocks
}

func fatal(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
		os.Exit(1)
	}
}
