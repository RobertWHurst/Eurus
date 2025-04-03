package main

import (
	"github.com/RobertWHurst/eurus"
	natstransport "github.com/RobertWHurst/eurus/nats-transport"
	"github.com/RobertWHurst/velaros"
	"github.com/nats-io/nats.go"
	"github.com/telemetrytv/Go-Shared/sync"
)

func main() {
	ex := sync.NewExitCoordinator()

	conn, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	router := velaros.NewRouter()
	service := eurus.NewService("example", natstransport.New(conn), router)

	router.PublicBind("/hello", func(ctx *velaros.Context) {
		ctx.Reply("Hello, world!")

		ctx.Send("How are you client?")
	})

	if err := service.Start(); err != nil {
		panic(err)
	}

	ex.UntilExit()

	if err := service.Stop(); err != nil {
		panic(err)
	}

	ex.ReadyToExit()
}
