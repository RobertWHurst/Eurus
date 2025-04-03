package natstransport

import (
	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
)

type NatsTransport struct {
	NatsConnection *nats.Conn

	unbindGatewayAnnounce       func() error
	unbindServiceAnnounce       func() error
	unbindInboundSocketMessage  map[string][]func() error
	unbindOutboundSocketMessage map[string][]func() error
	unbindSocketClose           map[string][]func() error
}

var _ eurus.Transport = &NatsTransport{}

func New(conn *nats.Conn) *NatsTransport {
	return &NatsTransport{
		NatsConnection: conn,

		unbindInboundSocketMessage:  map[string][]func() error{},
		unbindOutboundSocketMessage: map[string][]func() error{},
		unbindSocketClose:           map[string][]func() error{},
	}
}
