// Package api створений для роботи з користувачами поза p2p мережі
package api

import (
	"log"
	"strconv"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/core/database"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func StartServer(mempool *chain.Mempool, bs *database.BlockStorage) {
	app := fiber.New()

	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${ip}:${port} ${status} - ${method} ${path} ${latency} ${error} ${ua}\n",
	}))

	app.Get("/", func(c *fiber.Ctx) error {
		return c.Status(200).JSON(fiber.Map{
			"status": "ok",
			"error":  "",
		})
	})

	app.Get("/block/:id", func(c *fiber.Ctx) error {
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

		block, err := bs.GetBlock(blockHeight)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{
				// TODO: прибрати повертання внутрішньої помилка
				"status": "not ok, bro",
				"error":  "помилка отримання блоку",
			})
		}
		return c.JSON(block)
	})

	app.Post("/tx", func(c *fiber.Ctx) error {
		var tx chain.Transaction
		if err := c.BodyParser(&tx); err != nil {
			return c.Status(400).SendString("Invalid tx")
		}
		if err := mempool.Add(&tx); err != nil {
			return c.Status(400).SendString(err.Error())
		}
		return c.SendString("ok")
	})

	log.Fatal(app.Listen(":8080"))
}
