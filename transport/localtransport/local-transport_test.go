package localtransport

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus/v2"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve runs a subscription until the returned stop function is called.
// Subscribe methods register synchronously, so the handler is live as soon as
// this returns.
func serve(t *testing.T, subscribeFn func(ctx context.Context) (eurus.Subscription, error)) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	sub, err := subscribeFn(ctx)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- sub.Serve(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("subscription did not stop")
		}
	}
}

func TestLocalTransport_MessageService_DeliversMessageToHandler(t *testing.T) {
	transport := New()

	var receivedGatewayID string
	var receivedSocketID string
	var receivedConnInfo *velaros.ConnectionInfo
	var receivedMsg *velaros.SocketMessage

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceMessages(ctx, "service-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
			receivedGatewayID = gatewayID
			receivedSocketID = socketID
			receivedConnInfo = connInfo
			receivedMsg = msg
		})
	})
	defer stop()

	headers := http.Header{"X-Test": []string{"value"}}
	connInfo := &velaros.ConnectionInfo{
		Headers:    headers,
		RemoteAddr: "",
	}
	err := transport.MessageService("service-1", "gateway-1", "socket-1", connInfo, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test message"),
	})

	assert.NoError(t, err)
	assert.Equal(t, "gateway-1", receivedGatewayID)
	assert.Equal(t, "socket-1", receivedSocketID)
	assert.Equal(t, headers, receivedConnInfo.Headers)
	assert.Equal(t, websocket.MessageText, receivedMsg.Type)
	assert.Equal(t, []byte("test message"), receivedMsg.Data)
}

func TestLocalTransport_MessageService_HandlesNoHandler(t *testing.T) {
	transport := New()

	err := transport.MessageService("service-1", "gateway-1", "socket-1", &velaros.ConnectionInfo{}, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})

	assert.Error(t, err)
}

func TestLocalTransport_SubscribeServiceMessages_RegistersHandler(t *testing.T) {
	transport := New()

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceMessages(ctx, "service-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {})
	})

	assert.NotNil(t, transport.serviceMessageHandlers["service-1"])
	stop()
}

func TestLocalTransport_SubscribeServiceMessages_RemovesHandlerOnCancel(t *testing.T) {
	transport := New()

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceMessages(ctx, "service-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {})
	})
	stop()

	assert.Nil(t, transport.serviceMessageHandlers["service-1"])
}

func TestLocalTransport_MessageGateway_DeliversMessageToHandler(t *testing.T) {
	transport := New()

	var receivedSocketID string
	var receivedMsg *velaros.SocketMessage

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gateway-1", func(socketID string, msg *velaros.SocketMessage) {
			receivedSocketID = socketID
			receivedMsg = msg
		})
	})
	defer stop()

	err := transport.MessageGateway("gateway-1", "socket-1", &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test message"),
	})

	assert.NoError(t, err)
	assert.Equal(t, "socket-1", receivedSocketID)
	assert.Equal(t, websocket.MessageText, receivedMsg.Type)
	assert.Equal(t, []byte("test message"), receivedMsg.Data)
}

func TestLocalTransport_MessageGateway_HandlesNoHandler(t *testing.T) {
	transport := New()

	err := transport.MessageGateway("gateway-1", "socket-1", &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})

	assert.Error(t, err)
}

func TestLocalTransport_SubscribeGatewayMessages_RegistersHandler(t *testing.T) {
	transport := New()

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gateway-1", func(socketID string, msg *velaros.SocketMessage) {})
	})

	assert.NotNil(t, transport.gatewayMessageHandlers["gateway-1"])
	stop()
}

func TestLocalTransport_SubscribeGatewayMessages_RemovesHandlerOnCancel(t *testing.T) {
	transport := New()

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gateway-1", func(socketID string, msg *velaros.SocketMessage) {})
	})
	stop()

	assert.Nil(t, transport.gatewayMessageHandlers["gateway-1"])
}

func TestLocalTransport_ClosedSocket_NotifiesHandler(t *testing.T) {
	transport := New()

	var receivedSocketID string
	var receivedStatus websocket.StatusCode
	var receivedReason string

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketClosures(ctx, func(socketID string, status websocket.StatusCode, reason string) {
			receivedSocketID = socketID
			receivedStatus = status
			receivedReason = reason
		})
	})
	defer stop()

	err := transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "test reason")

	assert.NoError(t, err)
	assert.Equal(t, "socket-1", receivedSocketID)
	assert.Equal(t, websocket.StatusNormalClosure, receivedStatus)
	assert.Equal(t, "test reason", receivedReason)
}

func TestLocalTransport_ClosedSocket_NotifiesMultipleHandlers(t *testing.T) {
	transport := New()

	count := 0

	stop1 := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketClosures(ctx, func(socketID string, status websocket.StatusCode, reason string) {
			count++
		})
	})
	defer stop1()

	stop2 := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketClosures(ctx, func(socketID string, status websocket.StatusCode, reason string) {
			count++
		})
	})
	defer stop2()

	err := transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "test reason")

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestLocalTransport_SubscribeSocketClosures_RegistersHandler(t *testing.T) {
	transport := New()

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketClosures(ctx, func(socketID string, status websocket.StatusCode, reason string) {})
	})

	assert.Len(t, transport.socketClosedHandlers, 1)
	stop()
}

func TestLocalTransport_SubscribeSocketClosures_RemovesHandlerOnCancel(t *testing.T) {
	transport := New()

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketClosures(ctx, func(socketID string, status websocket.StatusCode, reason string) {})
	})
	stop()

	assert.Len(t, transport.socketClosedHandlers, 0)
}

func TestLocalTransport_New_ImplementsTransportInterface(t *testing.T) {
	transport := New()

	var _ eurus.Transport = transport
}

func TestLocalTransport_AnnounceGateway_NotifiesHandler(t *testing.T) {
	transport := New()

	var receivedDescriptor *eurus.GatewayDescriptor

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayAnnouncements(ctx, func(descriptor *eurus.GatewayDescriptor) {
			receivedDescriptor = descriptor
		})
	})
	defer stop()

	descriptor := &eurus.GatewayDescriptor{
		Name: "gateway-1",
	}

	err := transport.AnnounceGateway(descriptor)

	assert.NoError(t, err)
	assert.Equal(t, descriptor, receivedDescriptor)
}

func TestLocalTransport_AnnounceService_NotifiesHandler(t *testing.T) {
	transport := New()

	var receivedDescriptor *eurus.ServiceDescriptor

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceAnnouncements(ctx, func(descriptor *eurus.ServiceDescriptor) {
			receivedDescriptor = descriptor
		})
	})
	defer stop()

	descriptor := &eurus.ServiceDescriptor{
		Name: "service-1",
		ID:   "svc-1",
	}

	err := transport.AnnounceService(descriptor)

	assert.NoError(t, err)
	assert.Equal(t, descriptor, receivedDescriptor)
}

func TestLocalTransport_HeartbeatSockets_ReturnsDeadSockets(t *testing.T) {
	transport := New()

	stop := serve(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketHeartbeats(ctx, "gateway-1", func(serviceID string, socketIDs []string) []string {
			var dead []string
			for _, socketID := range socketIDs {
				if socketID != "alive-socket" {
					dead = append(dead, socketID)
				}
			}
			return dead
		})
	})
	defer stop()

	dead, err := transport.HeartbeatSockets("gateway-1", "service-1", []string{"alive-socket", "dead-socket-1", "dead-socket-2"})

	assert.NoError(t, err)
	assert.Equal(t, []string{"dead-socket-1", "dead-socket-2"}, dead)
}

func TestLocalTransport_HeartbeatSockets_ErrorsWhenGatewayUnavailable(t *testing.T) {
	transport := New()

	_, err := transport.HeartbeatSockets("gateway-1", "service-1", []string{"socket-1"})

	assert.Error(t, err)
}
