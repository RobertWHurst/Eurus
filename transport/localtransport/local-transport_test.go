package localtransport

import (
	"net/http"
	"testing"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
)

func TestLocalTransport_MessageService_DeliversMessageToHandler(t *testing.T) {
	transport := New()

	var receivedGatewayID string
	var receivedSocketID string
	var receivedConnInfo *velaros.ConnectionInfo
	var receivedMsg *velaros.SocketMessage

	transport.BindMessageService("service-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
		receivedGatewayID = gatewayID
		receivedSocketID = socketID
		receivedConnInfo = connInfo
		receivedMsg = msg
	})

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

	assert.NoError(t, err)
}

func TestLocalTransport_BindMessageService_RegistersHandler(t *testing.T) {
	transport := New()

	handler := func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
	}
	err := transport.BindMessageService("service-1", handler)

	assert.NoError(t, err)
	assert.NotNil(t, transport.serviceMessageHandlers["service-1"])
}

func TestLocalTransport_UnbindMessageService_RemovesHandler(t *testing.T) {
	transport := New()

	handler := func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
	}
	transport.BindMessageService("service-1", handler)

	err := transport.UnbindMessageService("service-1")

	assert.NoError(t, err)
	assert.Nil(t, transport.serviceMessageHandlers["service-1"])
}

func TestLocalTransport_MessageGateway_DeliversMessageToHandler(t *testing.T) {
	transport := New()

	var receivedSocketID string
	var receivedMsg *velaros.SocketMessage

	transport.BindMessageGateway("gateway-1", func(socketID string, msg *velaros.SocketMessage) {
		receivedSocketID = socketID
		receivedMsg = msg
	})

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

	assert.NoError(t, err)
}

func TestLocalTransport_BindMessageGateway_RegistersHandler(t *testing.T) {
	transport := New()

	handler := func(socketID string, msg *velaros.SocketMessage) {}
	err := transport.BindMessageGateway("gateway-1", handler)

	assert.NoError(t, err)
	assert.NotNil(t, transport.gatewayMessageHandlers["gateway-1"])
}

func TestLocalTransport_UnbindMessageGateway_RemovesHandler(t *testing.T) {
	transport := New()

	handler := func(socketID string, msg *velaros.SocketMessage) {}
	transport.BindMessageGateway("gateway-1", handler)

	err := transport.UnbindMessageGateway("gateway-1")

	assert.NoError(t, err)
	assert.Nil(t, transport.gatewayMessageHandlers["gateway-1"])
}

func TestLocalTransport_ClosedSocket_NotifiesHandler(t *testing.T) {
	transport := New()

	var receivedSocketID string
	var receivedStatus websocket.StatusCode
	var receivedReason string

	transport.BindSocketClosed(func(socketID string, status websocket.StatusCode, reason string) {
		receivedSocketID = socketID
		receivedStatus = status
		receivedReason = reason
	})

	err := transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "test reason")

	assert.NoError(t, err)
	assert.Equal(t, "socket-1", receivedSocketID)
	assert.Equal(t, websocket.StatusNormalClosure, receivedStatus)
	assert.Equal(t, "test reason", receivedReason)
}

func TestLocalTransport_ClosedSocket_NotifiesMultipleHandlers(t *testing.T) {
	transport := New()

	count := 0

	transport.BindSocketClosed(func(socketID string, status websocket.StatusCode, reason string) {
		count++
	})

	transport.BindSocketClosed(func(socketID string, status websocket.StatusCode, reason string) {
		count++
	})

	err := transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "test reason")

	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestLocalTransport_BindSocketClosed_RegistersHandler(t *testing.T) {
	transport := New()

	handler := func(socketID string, status websocket.StatusCode, reason string) {}
	err := transport.BindSocketClosed(handler)

	assert.NoError(t, err)
	assert.Len(t, transport.socketClosedHandlers, 1)
}

func TestLocalTransport_UnbindSocketClosed_RemovesHandlers(t *testing.T) {
	transport := New()

	handler := func(socketID string, status websocket.StatusCode, reason string) {}
	transport.BindSocketClosed(handler)

	err := transport.UnbindSocketClosed()

	assert.NoError(t, err)
	assert.Len(t, transport.socketClosedHandlers, 0)
}

func TestLocalTransport_New_ImplementsTransportInterface(t *testing.T) {
	transport := New()

	var _ eurus.Transport = transport
}

func TestLocalTransport_AnnounceGateway_NotifiesHandler(t *testing.T) {
	transport := New()

	var receivedDescriptor *eurus.GatewayDescriptor

	transport.BindGatewayAnnounce(func(descriptor *eurus.GatewayDescriptor) {
		receivedDescriptor = descriptor
	})

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

	transport.BindServiceAnnounce(func(descriptor *eurus.ServiceDescriptor) {
		receivedDescriptor = descriptor
	})

	descriptor := &eurus.ServiceDescriptor{
		Name: "service-1",
		ID:   "svc-1",
	}

	err := transport.AnnounceService(descriptor)

	assert.NoError(t, err)
	assert.Equal(t, descriptor, receivedDescriptor)
}
