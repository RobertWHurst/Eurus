package natstransport

import (
	"context"
	"errors"
	"time"

	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
)

type NatsTransport struct {
	NatsConnection *nats.Conn
}

var _ eurus.Transport = &NatsTransport{}

func New(natsConnection *nats.Conn) *NatsTransport {
	return &NatsTransport{
		NatsConnection: natsConnection,
	}
}

func signalReady(ready chan<- struct{}) {
	if ready == nil {
		return
	}
	select {
	case ready <- struct{}{}:
	default:
	}
}

func flushWithContext(ctx context.Context, conn *nats.Conn) error {
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.FlushWithContext(flushCtx); err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	return nil
}
