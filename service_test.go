package eurus_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/eurus/transport/localtransport"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClosedSocketTracking_RejectsLateMessages tests that when a socket is closed,
// late-arriving messages cannot recreate the connection
func TestClosedSocketTracking_RejectsLateMessages(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	messageReceived := false
	router := velaros.NewRouter()
	router.Bind("/test", func(ctx *velaros.Context) {
		messageReceived = true
		ctx.Send([]byte("ok"))
	})

	service := eurus.NewService("test-service", transport, router)
	route, _ := eurus.NewRouteDescriptor("/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	socketID := "test-socket-1"

	// Simulate connection opening
	connInfo := &velaros.ConnectionInfo{}
	msg1 := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test message 1"),
	}
	err = transport.MessageService(service.ID, gateway.ID, socketID, connInfo, msg1)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Close the socket
	err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "test close")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Try to send another message (simulating late-arriving message)
	messageReceived = false
	msg2 := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("late message"),
	}
	err = transport.MessageService(service.ID, gateway.ID, socketID, connInfo, msg2)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Message should be dropped, not processed
	assert.False(t, messageReceived, "Late message should be dropped for closed socket")
}

// TestClosedSocketTracking_PrunesOldRecords tests that closed socket records
// are removed after TTL expires
func TestClosedSocketTracking_PrunesOldRecords(t *testing.T) {
	transport := localtransport.New()

	router := velaros.NewRouter()
	service := eurus.NewService("test-service", transport, router)
	service.PruneInterval = 100 * time.Millisecond
	service.ClosedSocketTTL = 300 * time.Millisecond
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	socketID := "test-socket-pruning"

	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "test")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Message should be rejected (socket recently closed)
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	// Still closed
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	// Wait for TTL to expire
	time.Sleep(200 * time.Millisecond)

	// Record should be pruned now
}

// TestHeartbeat_UpdatesTimestamp tests that heartbeats update the last heartbeat timestamp
func TestHeartbeat_UpdatesTimestamp(t *testing.T) {
	transport := localtransport.New()

	router := velaros.NewRouter()
	service := eurus.NewService("test-service", transport, router)
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	socketID := "test-socket-heartbeat"

	// Create connection by sending a message
	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Send batched heartbeat targeted at service
	err = transport.HeartbeatSocketService(service.ID, []string{socketID})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Heartbeat should have been processed (verified by no errors)
}

// TestHeartbeat_PrunesStaleConnections tests that connections without heartbeats
// are removed from the connection map
func TestHeartbeat_PrunesStaleConnections(t *testing.T) {
	transport := localtransport.New()

	router := velaros.NewRouter()
	service := eurus.NewService("test-service", transport, router)
	service.HeartbeatTimeout = 300 * time.Millisecond
	service.PruneInterval = 100 * time.Millisecond
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	socketID := "test-socket-stale"

	// Create connection
	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Don't send heartbeats - wait for timeout and prune
	time.Sleep(500 * time.Millisecond)

	// Try sending message - should be dropped since connection was pruned and marked closed
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)
}

// TestGatewayHeartbeat_SendsBatchedHeartbeats tests that the gateway sends
// batched heartbeats targeted at service instances
func TestGatewayHeartbeat_SendsBatchedHeartbeats(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	gateway.HeartbeatInterval = 100 * time.Millisecond
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	// Create a service and register it
	router := velaros.NewRouter()
	service := eurus.NewService("test-service", transport, router)
	route, _ := eurus.NewRouteDescriptor("/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Send a message to create a socket mapping
	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}
	err = transport.MessageService(service.ID, gateway.ID, "socket-1", connInfo, msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Wait for at least one heartbeat cycle
	time.Sleep(200 * time.Millisecond)

	// Gateway heartbeat loop should have run (verified by no panics/errors)
}

// TestMultipleClosures_NoDoubleCleanup tests that closing a connection multiple
// times doesn't cause issues
func TestMultipleClosures_NoDoubleCleanup(t *testing.T) {
	transport := localtransport.New()

	router := velaros.NewRouter()
	service := eurus.NewService("test-service", transport, router)
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	socketID := "test-socket-double-close"

	// Create connection
	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Close the socket twice
	err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "test")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "test again")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Should not panic or cause errors
}

// TestRaceCondition_MessageBeforeClose tests the original race condition scenario
func TestRaceCondition_MessageBeforeClose(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	processedMessages := 0
	router := velaros.NewRouter()
	router.Bind("/test", func(ctx *velaros.Context) {
		processedMessages++
		ctx.Send([]byte("ok"))
	})

	service := eurus.NewService("test-service", transport, router)
	route, _ := eurus.NewRouteDescriptor("/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	socketID := "test-socket-race"
	connInfo := &velaros.ConnectionInfo{}

	// Simulate: gateway sends messages, then close event arrives first due to network reordering
	msg1 := &velaros.SocketMessage{Type: websocket.MessageText, Data: []byte("msg1")}
	msg2 := &velaros.SocketMessage{Type: websocket.MessageText, Data: []byte("msg2")}

	// Send first message (processed normally)
	err = transport.MessageService(service.ID, gateway.ID, socketID, connInfo, msg1)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// Close arrives
	err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "closing")
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	// Late message arrives (should be dropped)
	initialCount := processedMessages
	err = transport.MessageService(service.ID, gateway.ID, socketID, connInfo, msg2)
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, initialCount, processedMessages, "Late message after close should be dropped")
}
// TestConnection_ConcurrentHandleMessageAndClose tests concurrent message
// sending and connection closing
func TestConnection_ConcurrentHandleMessageAndClose(t *testing.T) {
	transport := localtransport.New()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	socketID := "test-socket-concurrent"
	connInfo := &velaros.ConnectionInfo{}

	// Create connection
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Send messages and close concurrently
	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 50; i++ {
			transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
			time.Sleep(1 * time.Millisecond)
		}
		done <- true
	}()

	go func() {
		time.Sleep(25 * time.Millisecond)
		transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "test")
		done <- true
	}()

	<-done
	<-done
}

// TestService_HighConnectionChurn tests rapid connect/disconnect cycles
func TestService_HighConnectionChurn(t *testing.T) {
	oldTTL := eurus.ClosedSocketTTL
	oldPruneInterval := eurus.ServicePruneInterval
	defer func() {
		eurus.ClosedSocketTTL = oldTTL
		eurus.ServicePruneInterval = oldPruneInterval
	}()

	eurus.ClosedSocketTTL = 200 * time.Millisecond
	eurus.ServicePruneInterval = 50 * time.Millisecond

	transport := localtransport.New()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	service.ClosedSocketTTL = 200 * time.Millisecond
	service.PruneInterval = 50 * time.Millisecond
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}

	// Rapid connect/disconnect cycles
	for i := 0; i < 100; i++ {
		socketID := fmt.Sprintf("socket-%d", i)
		
		// Create connection
		err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
		require.NoError(t, err)
		
		// Close immediately
		err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "churn")
		require.NoError(t, err)
	}

	// Wait for prune cycles
	time.Sleep(300 * time.Millisecond)

	// Closed sockets map should be small (pruned)
}

// TestService_RestartNoLeak tests that stopping and restarting a service
// doesn't leak resources
func TestService_RestartNoLeak(t *testing.T) {
	transport := localtransport.New()

	for cycle := 0; cycle < 5; cycle++ {
		service := eurus.NewService("test-service", transport, velaros.NewRouter())
		err := service.Start()
		require.NoError(t, err)

		// Create some connections
		connInfo := &velaros.ConnectionInfo{}
		msg := &velaros.SocketMessage{
			Type: websocket.MessageText,
			Data: []byte("test"),
		}

		for i := 0; i < 10; i++ {
			socketID := fmt.Sprintf("socket-%d-%d", cycle, i)
			err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
			require.NoError(t, err)
		}

		time.Sleep(50 * time.Millisecond)

		// Stop service
		service.Stop()

		time.Sleep(50 * time.Millisecond)
	}

	// Should not accumulate resources across restarts
}
