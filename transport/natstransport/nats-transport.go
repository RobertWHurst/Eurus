package natstransport

import (
	"sync"

	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
)

const DefaultMaxConcurrentHandlers = 256

type NatsTransport struct {
	NatsConnection               *nats.Conn
	unbindServiceAnnounce        func() error
	unbindGatewayAnnounce        func() error
	unbindMessageService         map[string]func() error
	unbindMessageGateway         map[string]func() error
	unbindSocketClosed           func() error
	unbindSocketHeartbeatService map[string]func() error
	messageHandlerWg             sync.WaitGroup
	messageParserSem                   chan struct{}
}

var _ eurus.Transport = &NatsTransport{}

func New(natsConnection *nats.Conn) *NatsTransport {
	return NewWithMaxConcurrency(natsConnection, DefaultMaxConcurrentHandlers)
}

func NewWithMaxConcurrency(natsConnection *nats.Conn, maxConcurrent int) *NatsTransport {
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrentHandlers
	}
	return &NatsTransport{
		NatsConnection:               natsConnection,
		unbindMessageService:         map[string]func() error{},
		unbindMessageGateway:         map[string]func() error{},
		unbindSocketHeartbeatService: map[string]func() error{},
		messageParserSem:                   make(chan struct{}, maxConcurrent),
	}
}
