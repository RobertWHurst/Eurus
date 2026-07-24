package natstransport

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startNats runs an embedded NATS server and returns a client connection.
func startNats(t *testing.T) *nats.Conn {
	t.Helper()
	server, err := natsserver.NewServer(&natsserver.Options{Port: -1})
	require.NoError(t, err)
	go server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		t.Fatal("embedded NATS server did not start")
	}
	t.Cleanup(server.Shutdown)

	conn, err := nats.Connect(server.ClientURL())
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn
}

// serveSub serves a subscription until the returned stop function is called.
func serveSub(t *testing.T, subscribeFn func(ctx context.Context) (eurus.Subscription, error)) (stop func()) {
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
		case <-time.After(5 * time.Second):
			t.Fatal("subscription did not stop")
		}
	}
}

func TestNatsTransport_MessageGateway_InlineRoundTrip(t *testing.T) {
	transport := New(startNats(t))

	received := make(chan *velaros.SocketMessage, 1)
	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gw-1", func(socketID string, msg *velaros.SocketMessage) {
			require.Equal(t, "socket-1", socketID)
			received <- msg
		})
	})
	defer stop()

	err := transport.MessageGateway("gw-1", "socket-1", &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("hello"),
		Meta: map[string]any{"k": "v"},
	})
	require.NoError(t, err)

	select {
	case msg := <-received:
		assert.Equal(t, websocket.MessageText, msg.Type)
		assert.Equal(t, []byte("hello"), msg.Data)
		assert.Equal(t, "v", msg.Meta["k"])
	case <-time.After(5 * time.Second):
		t.Fatal("message not delivered")
	}
}

func TestNatsTransport_MessageService_InlineRoundTrip(t *testing.T) {
	transport := New(startNats(t))

	type receivedMsg struct {
		gatewayID string
		socketID  string
		connInfo  *velaros.ConnectionInfo
		msg       *velaros.SocketMessage
	}
	received := make(chan receivedMsg, 1)
	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceMessages(ctx, "svc-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
			received <- receivedMsg{gatewayID, socketID, connInfo, msg}
		})
	})
	defer stop()

	connInfo := &velaros.ConnectionInfo{
		Headers:    map[string][]string{"X-Test": {"value"}},
		RemoteAddr: "10.0.0.1:1234",
	}
	err := transport.MessageService("svc-1", "gw-1", "socket-1", connInfo, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("payload"),
	})
	require.NoError(t, err)

	select {
	case got := <-received:
		assert.Equal(t, "gw-1", got.gatewayID)
		assert.Equal(t, "socket-1", got.socketID)
		assert.Equal(t, []string{"value"}, got.connInfo.Headers["X-Test"])
		assert.Equal(t, "10.0.0.1:1234", got.connInfo.RemoteAddr)
		assert.Equal(t, []byte("payload"), got.msg.Data)
	case <-time.After(5 * time.Second):
		t.Fatal("message not delivered")
	}
}

func TestNatsTransport_MessageGateway_OversizedPayloadStreams(t *testing.T) {
	conn := startNats(t)
	transport := New(conn)

	// Larger than the server's max payload so the chunk path must be used.
	bigPayload := bytes.Repeat([]byte("x"), int(conn.MaxPayload())+64*1024)

	received := make(chan *velaros.SocketMessage, 1)
	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gw-1", func(socketID string, msg *velaros.SocketMessage) {
			received <- msg
		})
	})
	defer stop()

	err := transport.MessageGateway("gw-1", "socket-1", &velaros.SocketMessage{
		Type: websocket.MessageBinary,
		Data: bigPayload,
	})
	require.NoError(t, err)

	select {
	case msg := <-received:
		assert.Equal(t, len(bigPayload), len(msg.Data))
		assert.True(t, bytes.Equal(bigPayload, msg.Data), "streamed payload should arrive intact")
	case <-time.After(10 * time.Second):
		t.Fatal("streamed message not delivered")
	}
}

func TestNatsTransport_PerSocketOrderingPreserved(t *testing.T) {
	transport := New(startNats(t))

	const messageCount = 50
	var mu sync.Mutex
	var order []string
	done := make(chan struct{})

	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gw-1", func(socketID string, msg *velaros.SocketMessage) {
			mu.Lock()
			order = append(order, string(msg.Data))
			if len(order) == messageCount {
				close(done)
			}
			mu.Unlock()
		})
	})
	defer stop()

	for i := range messageCount {
		err := transport.MessageGateway("gw-1", "socket-1", &velaros.SocketMessage{
			Type: websocket.MessageText,
			Data: fmt.Appendf(nil, "%d", i),
		})
		require.NoError(t, err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("only received %d of %d messages", len(order), messageCount)
	}

	for i, got := range order {
		require.Equal(t, fmt.Sprintf("%d", i), got, "messages for one socket must arrive in order")
	}
}

func TestNatsTransport_SlowSocketDoesNotBlockOthers(t *testing.T) {
	transport := New(startNats(t))

	slowStarted := make(chan struct{})
	slowRelease := make(chan struct{})
	fastReceived := make(chan struct{})

	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gw-1", func(socketID string, msg *velaros.SocketMessage) {
			switch socketID {
			case "slow-socket":
				close(slowStarted)
				<-slowRelease
			case "fast-socket":
				close(fastReceived)
			}
		})
	})
	defer stop()
	defer close(slowRelease)

	err := transport.MessageGateway("gw-1", "slow-socket", &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("slow"),
	})
	require.NoError(t, err)
	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("slow handler never started")
	}

	err = transport.MessageGateway("gw-1", "fast-socket", &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("fast"),
	})
	require.NoError(t, err)

	select {
	case <-fastReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("fast socket blocked behind slow socket")
	}
}

func TestNatsTransport_SlowConsumerOverflowClosesSocket(t *testing.T) {
	transport := New(startNats(t))
	transport.DispatchQueueSize = 2

	blockStarted := make(chan struct{})
	blockRelease := make(chan struct{})

	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayMessages(ctx, "gw-1", func(socketID string, msg *velaros.SocketMessage) {
			if string(msg.Data) == "block" {
				close(blockStarted)
				<-blockRelease
			}
		})
	})
	defer stop()
	// Release the blocked handler before stop() drains in-flight work.
	defer close(blockRelease)

	closures := make(chan string, 1)
	stopClosures := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketClosures(ctx, func(socketID string, status websocket.StatusCode, reason string) {
			select {
			case closures <- reason:
			default:
			}
		})
	})
	defer stopClosures()

	send := func(data string) error {
		return transport.MessageGateway("gw-1", "hog-socket", &velaros.SocketMessage{
			Type: websocket.MessageText,
			Data: []byte(data),
		})
	}

	// First message occupies the worker; wait until it is actually blocked so
	// the queue arithmetic below is deterministic.
	require.NoError(t, send("block"))
	select {
	case <-blockStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking handler never started")
	}

	// Fill the queue, then overflow it.
	require.NoError(t, send("queued-1"))
	require.NoError(t, send("queued-2"))
	err := send("overflow")
	require.ErrorIs(t, err, eurus.ErrSlowConsumer)

	select {
	case reason := <-closures:
		assert.Equal(t, "Slow consumer", reason)
	case <-time.After(5 * time.Second):
		t.Fatal("slow consumer closure was not broadcast")
	}
}

func TestNatsTransport_HeartbeatSockets_BatchRoundTrip(t *testing.T) {
	transport := New(startNats(t))

	requests := make(chan int, 4)
	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeSocketHeartbeats(ctx, "gw-1", func(serviceID string, socketIDs []string) []string {
			requests <- len(socketIDs)
			var dead []string
			for _, socketID := range socketIDs {
				if socketID != "alive" {
					dead = append(dead, socketID)
				}
			}
			return dead
		})
	})
	defer stop()

	dead, err := transport.HeartbeatSockets("gw-1", "svc-1", []string{"alive", "dead-1", "dead-2"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"dead-1", "dead-2"}, dead)
	assert.Equal(t, 3, <-requests, "all socket IDs should travel in one batched request")
}

func TestNatsTransport_HeartbeatSockets_ErrorsWhenNoGateway(t *testing.T) {
	transport := New(startNats(t))

	_, err := transport.HeartbeatSockets("no-such-gateway", "svc-1", []string{"socket-1"})
	assert.Error(t, err)
}

func TestNatsTransport_Announcements_RoundTrip(t *testing.T) {
	transport := New(startNats(t))

	gatewayAnnounces := make(chan *eurus.GatewayDescriptor, 1)
	stopGateway := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeGatewayAnnouncements(ctx, func(descriptor *eurus.GatewayDescriptor) {
			gatewayAnnounces <- descriptor
		})
	})
	defer stopGateway()

	serviceAnnounces := make(chan *eurus.ServiceDescriptor, 1)
	stopService := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceAnnouncements(ctx, func(descriptor *eurus.ServiceDescriptor) {
			serviceAnnounces <- descriptor
		})
	})
	defer stopService()

	require.NoError(t, transport.AnnounceGateway(&eurus.GatewayDescriptor{Name: "gw-1"}))
	require.NoError(t, transport.AnnounceService(&eurus.ServiceDescriptor{Name: "svc-1", ID: "svc-1-id"}))

	select {
	case descriptor := <-gatewayAnnounces:
		assert.Equal(t, "gw-1", descriptor.Name)
	case <-time.After(5 * time.Second):
		t.Fatal("gateway announcement not delivered")
	}
	select {
	case descriptor := <-serviceAnnounces:
		assert.Equal(t, "svc-1", descriptor.Name)
		assert.Equal(t, "svc-1-id", descriptor.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("service announcement not delivered")
	}
}

func BenchmarkNatsTransport_MessageGateway_Inline(b *testing.B) {
	server, err := natsserver.NewServer(&natsserver.Options{Port: -1})
	if err != nil {
		b.Fatal(err)
	}
	go server.Start()
	if !server.ReadyForConnections(5 * time.Second) {
		b.Fatal("embedded NATS server did not start")
	}
	defer server.Shutdown()

	conn, err := nats.Connect(server.ClientURL())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()

	transport := New(conn)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, err := transport.SubscribeGatewayMessages(ctx, "gw-bench", func(socketID string, msg *velaros.SocketMessage) {})
	if err != nil {
		b.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- sub.Serve(ctx) }()

	payload := bytes.Repeat([]byte("x"), 1024)
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if err := transport.MessageGateway("gw-bench", fmt.Sprintf("socket-%d", i%64), &velaros.SocketMessage{
			Type: websocket.MessageText,
			Data: payload,
		}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	cancel()
	<-done
}

func TestNatsTransport_MessageService_OversizedPayloadStreams(t *testing.T) {
	conn := startNats(t)
	transport := New(conn)

	bigPayload := bytes.Repeat([]byte("y"), int(conn.MaxPayload())+64*1024)

	received := make(chan *velaros.SocketMessage, 1)
	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceMessages(ctx, "svc-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
			received <- msg
		})
	})
	defer stop()

	err := transport.MessageService("svc-1", "gw-1", "socket-1", &velaros.ConnectionInfo{}, &velaros.SocketMessage{
		Type: websocket.MessageBinary,
		Data: bigPayload,
	})
	require.NoError(t, err)

	select {
	case msg := <-received:
		require.Equal(t, len(bigPayload), len(msg.Data))
		assert.True(t, bytes.Equal(bigPayload, msg.Data), "streamed payload should arrive intact")
	case <-time.After(10 * time.Second):
		t.Fatal("streamed message not delivered")
	}
}

func TestNatsTransport_ServiceAnnouncement_CarriesRouteDescriptors(t *testing.T) {
	transport := New(startNats(t))

	received := make(chan *eurus.ServiceDescriptor, 1)
	stop := serveSub(t, func(ctx context.Context) (eurus.Subscription, error) {
		return transport.SubscribeServiceAnnouncements(ctx, func(descriptor *eurus.ServiceDescriptor) {
			received <- descriptor
		})
	})
	defer stop()

	route, err := eurus.NewRouteDescriptor("/api/items/:id")
	require.NoError(t, err)
	require.NoError(t, transport.AnnounceService(&eurus.ServiceDescriptor{
		Name:             "svc-1",
		ID:               "svc-1-id",
		RouteDescriptors: []*eurus.RouteDescriptor{route},
	}))

	select {
	case descriptor := <-received:
		require.Len(t, descriptor.RouteDescriptors, 1)
		require.NotNil(t, descriptor.RouteDescriptors[0].Pattern)
		_, matched := descriptor.RouteDescriptors[0].Pattern.Match("/api/items/42")
		assert.True(t, matched, "route pattern must survive the msgpack round trip")
	case <-time.After(5 * time.Second):
		t.Fatal("announcement not delivered")
	}
}
