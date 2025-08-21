// Package api створений для роботи з користувачами поза p2p мережі
// Package api provides the HTTP API for interacting with the PQlite blockchain node.
package api

import (
	"encoding/hex"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/PQlite/core/p2p"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
)

// Server представляє HTTP-сервер API.
type Server struct {
	app     *fiber.App
	node    *p2p.Node
	mempool *chain.Mempool
	bs      *database.BlockStorage
}

// NewServer створює новий екземпляр API-сервера.
func NewServer(node *p2p.Node, mempool *chain.Mempool, bs *database.BlockStorage) *Server {
	app := fiber.New()

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
	}

	server.setupRoutes()
	return server
}

// setupRoutes реєструє всі обробники для маршрутів API.
func (s *Server) setupRoutes() {
	s.app.Get("/", s.handleGetStatus)
	s.app.Get("/block/:id", s.handleGetBlock)
	s.app.Get("/txs", s.handleGetMempoolLen)
	s.app.Get("/blocks", s.handleGetAllBlocks)
	s.app.Get("/addr/:id", s.handleGetBalance)
	s.app.Get("/lastBlock", s.handleGetLastBlock)
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
		"error":  "",
	})
}

func (s *Server) handleGetMempoolLen(c *fiber.Ctx) error {
	return c.SendString(strconv.Itoa(len(s.mempool.TXs)))
}

func (s *Server) handleGetBalance(c *fiber.Ctx) error {
	addr := c.Params("id")
	addrBytes, err := hex.DecodeString(addr)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": err,
		})
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

// Start запускає HTTP-сервер.
func (s *Server) Start() {
	log.Fatal().Err(s.app.Listen(":8081")).Msg("помилка запуску http серверу")
}
