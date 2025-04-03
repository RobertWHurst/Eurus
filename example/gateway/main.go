package main

import (
	"net/http"

	"github.com/RobertWHurst/eurus"
	natstransport "github.com/RobertWHurst/eurus/nats-transport"
	"github.com/nats-io/nats.go"
)

func main() {
	conn, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		panic(err)
	}

	gateway := eurus.NewGateway("example", natstransport.New(conn))

	http.Handle("/", gateway)

	gateway.Start()

	http.ListenAndServe(":8287", nil)
}
