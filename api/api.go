// Package api створений для роботи з користувачами поза p2p мережі
// Package api provides the HTTP API for interacting with the PQlite blockchain node.
package api

import (
	"log"
	"strconv"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/PQlite/core/p2p"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
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

	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${ip}:${port} ${status} - ${method} ${path} ${latency} ${error} ${ua}\n",
	}))

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
	s.app.Post("/tx", s.handlePostTx)
}

// handleGetStatus обробляє запит статусу.
func (s *Server) handleGetStatus(c *fiber.Ctx) error {
	// TODO: треба переписати відповіді, тому що зараз я повертаю код і стату. в цьому не має сенсу
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

// handlePostTx обробляє нову транзакцію.
func (s *Server) handlePostTx(c *fiber.Ctx) error {
	var tx chain.Transaction
	if err := c.BodyParser(&tx); err != nil {
		log.Println("помилка транзакції: ", err)
		return c.Status(400).SendString("Invalid tx")
	}

	s.node.TxCh <- &tx

	return c.Status(200).JSON(fiber.Map{
		"status": "ok",
		"error":  "",
	})
}

// Start запускає HTTP-сервер.
func (s *Server) Start() {
	log.Fatal(s.app.Listen(":8081"))
}
