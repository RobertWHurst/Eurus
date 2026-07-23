package localtransport

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
)

func waitReady(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("handler did not become ready")
	}
}

func cleanupHandler(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("handler did not stop")
	}
}

func TestLocalTransport_MessageService_DeliversMessageToHandler(t *testing.T) {
	transport := New()

	var receivedGatewayID string
	var receivedSocketID string
	var receivedConnInfo *velaros.ConnectionInfo
	var receivedMsg *velaros.SocketMessage

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleServiceMessages(ctx, ready, "service-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
			receivedGatewayID = gatewayID
			receivedSocketID = socketID
			receivedConnInfo = connInfo
			receivedMsg = msg
		})
	}()
	waitReady(t, ready)
	defer cleanupHandler(t, cancel, done)

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

func TestLocalTransport_HandleServiceMessages_RegistersHandler(t *testing.T) {
	transport := New()

	handler := func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleServiceMessages(ctx, ready, "service-1", handler)
	}()
	waitReady(t, ready)

	assert.NotNil(t, transport.serviceMessageHandlers["service-1"])
	cleanupHandler(t, cancel, done)
}

func TestLocalTransport_HandleServiceMessages_RemovesHandlerOnCancel(t *testing.T) {
	transport := New()

	handler := func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleServiceMessages(ctx, ready, "service-1", handler)
	}()
	waitReady(t, ready)

	cleanupHandler(t, cancel, done)

	assert.Nil(t, transport.serviceMessageHandlers["service-1"])
}

func TestLocalTransport_MessageGateway_DeliversMessageToHandler(t *testing.T) {
	transport := New()

	var receivedSocketID string
	var receivedMsg *velaros.SocketMessage

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleGatewayMessages(ctx, ready, "gateway-1", func(socketID string, msg *velaros.SocketMessage) {
			receivedSocketID = socketID
			receivedMsg = msg
		})
	}()
	waitReady(t, ready)
	defer cleanupHandler(t, cancel, done)

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

func TestLocalTransport_HandleGatewayMessages_RegistersHandler(t *testing.T) {
	transport := New()

	handler := func(socketID string, msg *velaros.SocketMessage) {}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleGatewayMessages(ctx, ready, "gateway-1", handler)
	}()
	waitReady(t, ready)

	assert.NotNil(t, transport.gatewayMessageHandlers["gateway-1"])
	cleanupHandler(t, cancel, done)
}

func TestLocalTransport_HandleGatewayMessages_RemovesHandlerOnCancel(t *testing.T) {
	transport := New()

	handler := func(socketID string, msg *velaros.SocketMessage) {}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleGatewayMessages(ctx, ready, "gateway-1", handler)
	}()
	waitReady(t, ready)

	cleanupHandler(t, cancel, done)

	assert.Nil(t, transport.gatewayMessageHandlers["gateway-1"])
}

func TestLocalTransport_ClosedSocket_NotifiesHandler(t *testing.T) {
	transport := New()

	var receivedSocketID string
	var receivedStatus websocket.StatusCode
	var receivedReason string

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleSocketClosures(ctx, ready, func(socketID string, status websocket.StatusCode, reason string) {
			receivedSocketID = socketID
			receivedStatus = status
			receivedReason = reason
		})
	}()
	waitReady(t, ready)
	defer cleanupHandler(t, cancel, done)

	err := transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "test reason")

	assert.NoError(t, err)
	assert.Equal(t, "socket-1", receivedSocketID)
	assert.Equal(t, websocket.StatusNormalClosure, receivedStatus)
	assert.Equal(t, "test reason", receivedReason)
}

func TestLocalTransport_ClosedSocket_NotifiesMultipleHandlers(t *testing.T) {
	transport := New()

	count := 0

	ctx, cancel := context.WithCancel(context.Background())
	ready1 := make(chan struct{}, 1)
	done1 := make(chan error, 1)
	go func() {
		done1 <- transport.HandleSocketClosures(ctx, ready1, func(socketID string, status websocket.StatusCode, reason string) {
			count++
		})
	}()
	waitReady(t, ready1)

	ready2 := make(chan struct{}, 1)
	done2 := make(chan error, 1)
	go func() {
		done2 <- transport.HandleSocketClosures(ctx, ready2, func(socketID string, status websocket.StatusCode, reason string) {
			count++
		})
	}()
	waitReady(t, ready2)
	defer cleanupHandler(t, cancel, done1)
	defer cleanupHandler(t, cancel, done2)

	err := transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "test reason")

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestLocalTransport_HandleSocketClosures_RegistersHandler(t *testing.T) {
	transport := New()

	handler := func(socketID string, status websocket.StatusCode, reason string) {}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleSocketClosures(ctx, ready, handler)
	}()
	waitReady(t, ready)

	assert.Len(t, transport.socketClosedHandlers, 1)
	cleanupHandler(t, cancel, done)
}

func TestLocalTransport_HandleSocketClosures_RemovesHandlerOnCancel(t *testing.T) {
	transport := New()

	handler := func(socketID string, status websocket.StatusCode, reason string) {}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleSocketClosures(ctx, ready, handler)
	}()
	waitReady(t, ready)

	cleanupHandler(t, cancel, done)

	assert.Len(t, transport.socketClosedHandlers, 0)
}

func TestLocalTransport_New_ImplementsTransportInterface(t *testing.T) {
	transport := New()

	var _ eurus.Transport = transport
}

func TestLocalTransport_AnnounceGateway_NotifiesHandler(t *testing.T) {
	transport := New()

	var receivedDescriptor *eurus.GatewayDescriptor

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleGatewayAnnouncements(ctx, ready, func(descriptor *eurus.GatewayDescriptor) {
			receivedDescriptor = descriptor
		})
	}()
	waitReady(t, ready)
	defer cleanupHandler(t, cancel, done)

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

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- transport.HandleServiceAnnouncements(ctx, ready, func(descriptor *eurus.ServiceDescriptor) {
			receivedDescriptor = descriptor
		})
	}()
	waitReady(t, ready)
	defer cleanupHandler(t, cancel, done)

	descriptor := &eurus.ServiceDescriptor{
		Name: "service-1",
		ID:   "svc-1",
	}

	err := transport.AnnounceService(descriptor)

	assert.NoError(t, err)
	assert.Equal(t, descriptor, receivedDescriptor)
}
