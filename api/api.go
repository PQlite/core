// Package api створений для роботи з користувачами поза p2p мережі
// Package api provides the HTTP API for interacting with the PQlite blockchain node.
package api

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/PQlite/core/p2p"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/websocket/v2"
	"github.com/rs/zerolog/log"
	"sync"
)

// Server представляє HTTP-сервер API.
type Server struct {
	app     *fiber.App
	node    *p2p.Node
	mempool *chain.Mempool
	bs      *database.BlockStorage
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

// NewServer створює новий екземпляр API-сервера.
func NewServer(node *p2p.Node, mempool *chain.Mempool, bs *database.BlockStorage) *Server {
	app := fiber.New()

	// Rate limiting: 100 запитів на 1 хвилину з одного IP
	app.Use(limiter.New(limiter.Config{
		Max:        1000,
		Expiration: 1 * time.Minute,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(429).JSON(fiber.Map{
				"error": "Забагато запитів. Спробуйте пізніше.",
			})
		},
	}))

	app.Use(func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		statusCode := c.Response().StatusCode()
		event := log.Info()
		if err != nil {
			event = log.Error().Err(err)
		}

		event.Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", statusCode).
			Dur("latency", time.Since(start)).
			Str("ip", c.IP()).
			Str("user_agent", c.Get("User-Agent")).
			Msg("request")

		return err
	})

	server := &Server{
		app:     app,
		node:    node,
		mempool: mempool,
		bs:      bs,
		clients: make(map[*websocket.Conn]bool),
	}

	go server.runWebSocketPoller()

	server.setupRoutes()
	return server
}

// setupRoutes реєструє всі обробники для маршрутів API.
func (s *Server) setupRoutes() {
	s.app.Static("/", "./public")
	s.app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		s.mu.Lock()
		s.clients[c] = true
		s.mu.Unlock()

		s.sendFullState(c)

		defer func() {
			s.mu.Lock()
			delete(s.clients, c)
			s.mu.Unlock()
			c.Close()
		}()

		// Keep connection alive
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break
			}
		}
	}))
	s.app.Get("/status", s.handleGetStatus)
	s.app.Get("/block/:id", s.handleGetBlock)
	s.app.Get("/txs", s.handleGetMempoolLen)
	s.app.Get("/blocks", s.handleGetAllBlocks)
	s.app.Get("/addr/:id", s.handleGetBalance)
	s.app.Get("/validators", s.handleGetValidators)
	s.app.Get("/wallets", s.handleGetAllWallets)
	s.app.Get("/lastBlock", s.handleGetLastBlock)
	s.app.Get("/chainSize", s.handleGetChainSize)
	s.app.Get("/nextProposer", s.handleGetNextProposer)
	s.app.Get("/currentRound", s.handleGetCurrentRound)
	s.app.Get("/tx/:hash", s.handleGetTxStatus)
	s.app.Post("/tx", s.handlePostTx)

	// щоб сервер не відповідав усіляким підораскам
	s.app.Use(func(c *fiber.Ctx) error {
		time.Sleep(100 * time.Second)
		hijacker, ok := c.Context().Conn().(*net.TCPConn)
		if ok {
			_ = hijacker.Close()
		}
		return nil
	})
}

// handleGetStatus обробляє запит статусу.
func (s *Server) handleGetStatus(c *fiber.Ctx) error {
	// TODO: треба переписати відповіді, тому що зараз я повертаю код і статус. в цьому не має сенсу
	return c.Status(200).JSON(fiber.Map{
		"status": "ok",
		"error":  "",
	})
}

// handleGetBlock обробляє запит на отримання блоку.
func (s *Server) handleGetBlock(c *fiber.Ctx) error {
	blockheightStr := c.Params("id")

	// Конвертація в uint32
	blockHeight64, err := strconv.ParseUint(blockheightStr, 10, 32)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"status": "not ok, bro",
			"error":  "Invalid ID format",
		})
	}
	blockHeight := uint32(blockHeight64)

	block, err := s.bs.GetBlock(blockHeight)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"status": "not ok, bro",
			"error":  "помилка отримання блоку",
		})
	}
	return c.JSON(block)
}

func (s *Server) handleGetAllBlocks(c *fiber.Ctx) error {
	blocks, err := s.bs.GetAllBlocks()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	// сортувати по зростанню номера блока
	sort.Slice(blocks, func(i int, j int) bool {
		return blocks[i].Height < blocks[j].Height
	})

	return c.JSON(blocks)
}

// handlePostTx обробляє нову транзакцію.
func (s *Server) handlePostTx(c *fiber.Ctx) error {
	var tx chain.Transaction
	if err := c.BodyParser(&tx); err != nil {
		log.Error().Err(err).Msg("помилка транзакції")
		return c.Status(400).SendString("Invalid tx")
	}

	s.node.TxCh <- &tx

	return c.Status(200).JSON(fiber.Map{
		"status": "ok",
		"hash":   hex.EncodeToString(tx.Hash()),
	})
}

func (s *Server) handleGetTxStatus(c *fiber.Ctx) error {
	hashHex := c.Params("hash")
	hash, err := hex.DecodeString(hashHex)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid hash"})
	}

	// Перевірка в блоках
	height, err := s.bs.GetTxBlock(hash)
	if err == nil {
		return c.JSON(fiber.Map{
			"status": "confirmed",
			"height": height,
		})
	}

	// Перевірка в mempool
	txs := s.mempool.GetTransactions()
	for _, tx := range txs {
		if bytes.Equal(tx.Hash(), hash) {
			return c.JSON(fiber.Map{
				"status": "pending",
			})
		}
	}

	return c.Status(404).JSON(fiber.Map{
		"status": "not_found",
	})
}

func (s *Server) handleGetMempoolLen(c *fiber.Ctx) error {
	txs := s.mempool.GetTransactions()
	return c.JSON(txs)
}

func (s *Server) handleGetBalance(c *fiber.Ctx) error {
	addr := c.Params("id")
	addr = strings.TrimPrefix(addr, "0x")
	addrBytes, err := hex.DecodeString(addr)
	if err != nil {
		// Try base64 if hex fails
		addrBytes, err = base64.StdEncoding.DecodeString(addr)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{
				"error": "Invalid address format (must be hex or base64)",
			})
		}
	}

	wallet, err := s.bs.GetWalletByAddress(addrBytes)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": err,
		})
	}
	return c.Status(200).JSON(wallet)
}

func (s *Server) handleGetLastBlock(c *fiber.Ctx) error {
	lastBlock, err := s.bs.GetLastBlock()
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": err,
		})
	}
	return c.JSON(lastBlock)
}

func (s *Server) handleGetChainSize(c *fiber.Ctx) error {
	size, err := s.bs.GetSize()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"size_mb": float64(size) / (1024 * 1024),
	})
}

func (s *Server) handleGetValidators(c *fiber.Ctx) error {
	validators, err := s.bs.GetValidatorsList()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	var res []fiber.Map
	for _, v := range *validators {
		wallet, _ := s.bs.GetWalletByAddress(v.Address)
		res = append(res, fiber.Map{
			"address": v.Address,
			"stake":   v.Amount,
			"balance": wallet.Balance,
		})
	}

	return c.JSON(res)
}

func (s *Server) handleGetAllWallets(c *fiber.Ctx) error {
	wallets, err := s.bs.GetAllWallets()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
	return c.JSON(wallets)
}

func (s *Server) handleGetNextProposer(c *fiber.Ctx) error {
	return c.JSON(s.node.GetNextProposer())
}

func (s *Server) handleGetCurrentRound(c *fiber.Ctx) error {
	return c.JSON(s.node.GetCurrentRound())
}

func (s *Server) runWebSocketPoller() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Ініціалізація поточною висотою
	lastBlock, err := s.bs.GetLastBlock()
	var lastHeight uint32 = 0
	if err == nil {
		lastHeight = lastBlock.Height
	}

	for range ticker.C {
		// Перевірка нових блоків
		lastBlock, err := s.bs.GetLastBlock()
		if err == nil && lastBlock.Height > lastHeight {
			lastHeight = lastBlock.Height

			s.mu.Lock()
			for client := range s.clients {
				s.sendFullState(client)
			}
			s.mu.Unlock()
		}
	}
}

// Допоміжна функція для відправки повного стану новому клієнту
func (s *Server) sendFullState(c *websocket.Conn) {
	blocks, _ := s.bs.GetAllBlocks()
	// Сортуємо блоки по висоті для коректного відображення в UI
	sort.Slice(blocks, func(i, j int) bool {
		return blocks[i].Height < blocks[j].Height
	})

	validators, _ := s.bs.GetValidatorsList()
	var valList []fiber.Map
	for _, v := range *validators {
		wallet, _ := s.bs.GetWalletByAddress(v.Address)
		valList = append(valList, fiber.Map{
			"address": v.Address,
			"stake":   v.Amount,
			"balance": wallet.Balance,
		})
	}

	wallets, _ := s.bs.GetAllWallets()
	lastBlock, _ := s.bs.GetLastBlock()
	mempool := s.mempool.GetTransactions()
	size, _ := s.bs.GetSize()

	state := fiber.Map{
		"stats": fiber.Map{
			"lastHeight":    lastBlock.Height,
			"mempoolSize":   len(mempool),
			"currentRound":  s.node.GetCurrentRound(),
			"nextProposer":  s.node.GetNextProposer(),
			"sizeMb":        float64(size) / (1024 * 1024),
		},
		"blocks":     blocks,
		"validators": valList,
		"wallets":    wallets,
	}

	data, _ := json.Marshal(state)
	c.WriteMessage(websocket.TextMessage, data)
}
// Start запускає HTTP-сервер.
func (s *Server) Start() {
	log.Fatal().Err(s.app.Listen(":8081")).Msg("помилка запуску http серверу")
}
