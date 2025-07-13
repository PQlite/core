package api

import (
	"log"
	"strconv"

	"github.com/PQlite/core/chain"
	"github.com/PQlite/crypto"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
)

func StartServer(mempool *chain.Mempool) {
	app := fiber.New()

	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${ip}:${port} ${status} - ${method} ${path} ${latency} ${error} ${ua}\n",
	}))

	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendString(strconv.Itoa(mempool.Len()))
	})

	app.Post("/tx", func(c *fiber.Ctx) error {
		var tx crypto.Transaction
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
