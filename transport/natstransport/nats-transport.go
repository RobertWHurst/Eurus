package natstransport

import (
	"context"
	"time"

	"github.com/RobertWHurst/eurus/v2"
	"github.com/nats-io/nats.go"
)

// DefaultDispatchQueueSize is the default per-socket pending message queue
// size. When a socket's queue overflows the socket is closed as a slow
// consumer.
const DefaultDispatchQueueSize = 128

const flushTimeout = 5 * time.Second

type NatsTransport struct {
	NatsConnection *nats.Conn

	// DispatchQueueSize bounds the number of pending messages per socket on
	// the receiving side. Zero means DefaultDispatchQueueSize.
	DispatchQueueSize int
}

var _ eurus.Transport = &NatsTransport{}

func New(natsConnection *nats.Conn) *NatsTransport {
	return &NatsTransport{
		NatsConnection: natsConnection,
	}
}

func (t *NatsTransport) dispatchQueueSize() int {
	if t.DispatchQueueSize > 0 {
		return t.DispatchQueueSize
	}
	return DefaultDispatchQueueSize
}

// flush confirms the server has processed everything sent so far, bounding
// the wait with flushTimeout and the given context.
func (t *NatsTransport) flush(ctx context.Context) error {
	flushCtx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	return t.NatsConnection.FlushWithContext(flushCtx)
}

// subscribeAsync subscribes with an async handler and returns a Subscription
// that unsubscribes when its serve context is canceled.
func (t *NatsTransport) subscribeAsync(ctx context.Context, subject string, cb nats.MsgHandler) (eurus.Subscription, error) {
	sub, err := t.NatsConnection.Subscribe(subject, cb)
	if err != nil {
		return nil, err
	}
	if err := t.flush(ctx); err != nil {
		_ = sub.Unsubscribe()
		return nil, err
	}
	return eurus.SubscriptionFunc(func(ctx context.Context) error {
		<-ctx.Done()
		if err := sub.Unsubscribe(); err != nil {
			transportNatsDebug.Tracef("Failed to unsubscribe from %s during shutdown: %v", subject, err)
		}
		return nil
	}), nil
}
