// CLI для взаємодії з PQlite нодою.
// Використання: pqlite <команда> [прапори]
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
	"strconv"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/crypto"
)

const defaultNode = "http://localhost:8081"
const defaultKeyFile = ".env"

type keyFile struct {
	Priv []byte `json:"priv"`
	Pub  []byte `json:"pub"`
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "keygen":
		cmdKeygen(os.Args[2:])
	case "balance":
		cmdBalance(os.Args[2:])
	case "send":
		cmdSend(os.Args[2:])
	case "block":
		cmdBlock(os.Args[2:])
	case "blocks":
		cmdBlocks(os.Args[2:])
	case "mempool":
		cmdMempool(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "невідома команда: %s\n\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`PQlite CLI

Команди:
  keygen  [-out <файл>]                           Згенерувати новий ключ
  balance <hex_адреса>  [-node <url>]             Перевірити баланс
  send    -to <hex_адреса> -amount <n> [-fee <n>]
          [-key <файл>] [-nonce <n>] [-node <url>]
  block   <висота>  [-node <url>]                 Отримати блок
  blocks  [-node <url>]                           Список усіх блоків
  mempool [-node <url>]                           Розмір mempool
  status  [-node <url>]                           Статус ноди
`)
}

// --- keygen ---

func cmdKeygen(args []string) {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "", "файл для збереження (за замовчуванням — вивести на екран)")
	fs.Parse(args)

	pub, priv, err := crypto.Create()
	fatal(err, "помилка генерації ключа")

	kf := keyFile{Priv: priv, Pub: pub}
	data, err := json.MarshalIndent(kf, "", "  ")
	fatal(err, "помилка серіалізації")

	if *out != "" {
		err = os.WriteFile(*out, data, 0600)
		fatal(err, "помилка запису файлу")
		fmt.Printf("Ключ збережено: %s\n", *out)
	} else {
		fmt.Println(string(data))
	}

	fmt.Printf("Публічний ключ (hex): %s\n", hex.EncodeToString(pub))
}

// --- balance ---

func cmdBalance(args []string) {
	fs := flag.NewFlagSet("balance", flag.ExitOnError)
	node := fs.String("node", defaultNode, "адреса ноди")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "вкажіть hex адресу: pqlite balance <hex_адреса>")
		os.Exit(1)
	}

	addr := fs.Arg(0)
	url := fmt.Sprintf("%s/addr/%s", *node, addr)
	resp, err := http.Get(url)
	fatal(err, "помилка запиту")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "помилка: %s\n", body)
		os.Exit(1)
	}

	var wallet chain.Wallet
	fatal(json.Unmarshal(body, &wallet), "помилка відповіді")

	fmt.Printf("Адреса: %s\n", hex.EncodeToString(wallet.Address))
	fmt.Printf("Баланс: %s PQL\n", chain.FormatAmount(wallet.Balance))
	fmt.Printf("Nonce:  %d\n", wallet.Nonce)
}

// --- send ---

func cmdSend(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	keyPath := fs.String("key", defaultKeyFile, "файл з ключами")
	toHex := fs.String("to", "", "адреса отримувача (hex)")
	amountFloat := fs.Float64("amount", 0, "сума")
	feeFloat := fs.Float64("fee", 0.01, "комісія")
	nonceFlag := fs.Uint("nonce", 0, "nonce (0 = автоматично)")
	node := fs.String("node", defaultNode, "адреса ноди")
	fs.Parse(args)

	if *toHex == "" || *amountFloat <= 0 {
		fmt.Fprintln(os.Stderr, "вкажіть -to та -amount")
		fs.Usage()
		os.Exit(1)
	}

	amount := int64(*amountFloat * float64(chain.Precision))
	fee := int64(*feeFloat * float64(chain.Precision))

	kf := loadKey(*keyPath)

	toBytes, err := hex.DecodeString(*toHex)
	fatal(err, "невірна hex адреса отримувача")

	nonce := uint32(*nonceFlag)
	if nonce == 0 {
		nonce = fetchNextNonce(*node, kf.Pub)
	}

	tx := chain.Transaction{
		From:      kf.Pub,
		To:        toBytes,
		Amount:    amount,
		Fee:       fee,
		Timestamp: time.Now().UnixMilli(),
		Nonce:     nonce,
	}
	fatal(tx.Sign(kf.Priv), "помилка підпису")

	data, err := json.Marshal(tx)
	fatal(err, "помилка серіалізації транзакції")

	resp, err := http.Post(*node+"/tx", "application/json", bytes.NewReader(data))
	fatal(err, "помилка відправки")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "помилка від ноди: %s\n", body)
		os.Exit(1)
	}

	fmt.Printf("Транзакцію відправлено\n")
	fmt.Printf("Від:   %s\n", hex.EncodeToString(kf.Pub))
	fmt.Printf("Кому:  %s\n", *toHex)
	fmt.Printf("Сума:  %s PQL\n", chain.FormatAmount(amount))
	fmt.Printf("Комісія: %s PQL\n", chain.FormatAmount(fee))
	fmt.Printf("Nonce: %d\n", nonce)
}

// --- block ---

func cmdBlock(args []string) {
	fs := flag.NewFlagSet("block", flag.ExitOnError)
	node := fs.String("node", defaultNode, "адреса ноди")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "вкажіть висоту: pqlite block <висота>")
		os.Exit(1)
	}

	resp, err := http.Get(fmt.Sprintf("%s/block/%s", *node, fs.Arg(0)))
	fatal(err, "помилка запиту")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "помилка: %s\n", body)
		os.Exit(1)
	}

	var block chain.Block
	fatal(json.Unmarshal(body, &block), "помилка відповіді")
	printBlock(&block)
}

// --- blocks ---

func cmdBlocks(args []string) {
	fs := flag.NewFlagSet("blocks", flag.ExitOnError)
	node := fs.String("node", defaultNode, "адреса ноди")
	fs.Parse(args)

	resp, err := http.Get(*node + "/blocks")
	fatal(err, "помилка запиту")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "помилка: %s\n", body)
		os.Exit(1)
	}

	var blocks []chain.Block
	fatal(json.Unmarshal(body, &blocks), "помилка відповіді")

	if len(blocks) == 0 {
		fmt.Println("блоків немає")
		return
	}

	fmt.Printf("%-6s %-20s %-8s %s\n", "Height", "Час", "Txs", "Hash")
	fmt.Println("--------------------------------------------------------------")
	for _, b := range blocks {
		ts := ""
		if b.Timestamp > 0 {
			ts = time.UnixMilli(b.Timestamp).Format("2006-01-02 15:04:05")
		}
		fmt.Printf("%-6d %-20s %-8d %s\n",
			b.Height, ts, len(b.Transactions), hex.EncodeToString(b.Hash))
	}
}

// --- mempool ---

func cmdMempool(args []string) {
	fs := flag.NewFlagSet("mempool", flag.ExitOnError)
	node := fs.String("node", defaultNode, "адреса ноди")
	fs.Parse(args)

	resp, err := http.Get(*node + "/txs")
	fatal(err, "помилка запиту")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	n, err := strconv.Atoi(string(bytes.TrimSpace(body)))
	if err != nil {
		fmt.Printf("mempool: %s\n", body)
		return
	}
	fmt.Printf("Транзакцій в mempool: %d\n", n)
}

// --- status ---

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	node := fs.String("node", defaultNode, "адреса ноди")
	fs.Parse(args)

	resp, err := http.Get(*node + "/")
	fatal(err, "нода недоступна")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Нода: %s\nВідповідь: %s\n", *node, body)
}

// --- helpers ---

func loadKey(path string) keyFile {
	data, err := os.ReadFile(path)
	fatal(err, "не вдалось прочитати файл ключів: "+path)
	var kf keyFile
	fatal(json.Unmarshal(data, &kf), "невірний формат файлу ключів")
	return kf
}

func fetchNextNonce(node string, pub []byte) uint32 {
	url := fmt.Sprintf("%s/addr/%s", node, hex.EncodeToString(pub))
	resp, err := http.Get(url)
	fatal(err, "помилка отримання nonce")
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var wallet chain.Wallet
	fatal(json.Unmarshal(body, &wallet), "помилка відповіді при отриманні nonce")
	return wallet.Nonce + 1
}

func printBlock(b *chain.Block) {
	ts := ""
	if b.Timestamp > 0 {
		ts = time.UnixMilli(b.Timestamp).Format("2006-01-02 15:04:05")
	}
	fmt.Printf("Висота:    %d\n", b.Height)
	fmt.Printf("Час:       %s\n", ts)
	fmt.Printf("Hash:      %s\n", hex.EncodeToString(b.Hash))
	fmt.Printf("PrevHash:  %s\n", hex.EncodeToString(b.PrevHash))
	fmt.Printf("Proposer:  %s\n", hex.EncodeToString(b.Proposer))
	fmt.Printf("Транзакцій: %d\n", len(b.Transactions))
}

func fatal(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
		os.Exit(1)
	}
}
