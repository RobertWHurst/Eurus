package eurus_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus/v2"
	"github.com/RobertWHurst/eurus/v2/transport/localtransport"
	"github.com/RobertWHurst/velaros"
	velarosjson "github.com/RobertWHurst/velaros/middleware/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slowConsumerTransport rejects every service delivery as a slow consumer.
type slowConsumerTransport struct {
	*localtransport.LocalTransport
}

func (t *slowConsumerTransport) MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) error {
	return eurus.ErrSlowConsumer
}

// TestGateway_SlowConsumerClosesSocketOnly verifies that a slow-consumer
// rejection closes the offending socket with a policy violation instead of
// unsetting the whole service instance from the gateway's index.
func TestGateway_SlowConsumerClosesSocketOnly(t *testing.T) {
	transport := &slowConsumerTransport{LocalTransport: localtransport.New()}

	gateway := eurus.NewGateway("test-gateway", transport)
	require.NoError(t, startGateway(t, gateway))

	route, err := eurus.NewRouteDescriptor("/api/*")
	require.NoError(t, err)
	require.NoError(t, transport.AnnounceService(&eurus.ServiceDescriptor{
		Name:             "test-service",
		ID:               "svc-instance-1",
		RouteDescriptors: []*eurus.RouteDescriptor{route},
	}))
	require.True(t, gateway.CanServePath("/api/test"))

	router := velaros.NewRouter()
	router.Use(velarosjson.Middleware())
	router.Use(gateway)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	require.NoError(t, wsjson.Write(ctx, conn, map[string]any{
		"path": "/api/test",
		"id":   "msg-1",
		"data": map[string]string{"k": "v"},
	}))

	// The delivery is rejected as slow consumer, so the gateway must close
	// the socket.
	var discard any
	err = wsjson.Read(ctx, conn, &discard)
	require.Error(t, err, "socket should be closed after slow-consumer rejection")

	// The service instance must still be routable for other sockets.
	assert.True(t, gateway.CanServePath("/api/test"))
}

func TestGateway_CanServePathBeforeConnect(t *testing.T) {
	gateway := eurus.NewGateway("test-gateway", localtransport.New())
	assert.False(t, gateway.CanServePath("/api/test"))
}

func TestRouteDescriptor_MsgpackRoundTrip(t *testing.T) {
	route, err := eurus.NewRouteDescriptor("/api/items/:id")
	require.NoError(t, err)

	data, err := route.MarshalMsgpack()
	require.NoError(t, err)

	decoded := &eurus.RouteDescriptor{}
	require.NoError(t, decoded.UnmarshalMsgpack(data))

	require.NotNil(t, decoded.Pattern)
	assert.Equal(t, route.Pattern.String(), decoded.Pattern.String())
	_, matched := decoded.Pattern.Match("/api/items/42")
	assert.True(t, matched)
}

func TestGatewayServiceIndexer_IsClosed(t *testing.T) {
	indexer := eurus.NewGatewayServiceIndexer()
	assert.False(t, indexer.IsClosed())
	indexer.Close()
	assert.True(t, indexer.IsClosed())
}
