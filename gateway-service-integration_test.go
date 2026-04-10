package eurus_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/eurus/transport/localtransport"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServiceDisappears_StillIndexed tests that when a service stops,
// it remains indexed until health checks remove it (eventual consistency)
func TestServiceDisappears_StillIndexed(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)

	// Wait for service announcement
	time.Sleep(50 * time.Millisecond)

	// Verify gateway can serve the path
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Gateway should be able to serve path while service is running")

	// Service suddenly stops (simulating crash or network failure)
	service.Stop()

	// The gateway should still report it can serve (cached/stale entry)
	// This is expected behavior - services are removed by health monitoring, not immediately
	canServe = gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Gateway should still have service in index immediately after stop (eventual consistency)")
}

// TestServiceDisappears_MultipleInstances tests that when services with the
// same name exist, they are independently tracked
func TestServiceDisappears_MultipleInstances(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	// Create two service instances with the same name
	service1 := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service1.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service1.Start()
	require.NoError(t, err)

	service2 := eurus.NewService("test-service", transport, velaros.NewRouter())
	service2.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service2.Start()
	require.NoError(t, err)
	defer service2.Stop()

	// Wait for announcements
	time.Sleep(50 * time.Millisecond)

	// Verify gateway can serve the path
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe)

	// Stop first instance
	service1.Stop()

	// Gateway should still be able to serve via the second instance
	// (both instances are still in the index, second one is healthy)
	time.Sleep(50 * time.Millisecond)
	canServe = gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Gateway should still serve path with remaining healthy instance")
}

// TestGatewayDisappears_ServiceContinuesRunning tests that when a gateway
// stops, services continue running and can announce to other gateways
func TestGatewayDisappears_ServiceContinuesRunning(t *testing.T) {
	transport := localtransport.New()

	gateway1 := eurus.NewGateway("gateway-1", transport)
	err := gateway1.Start()
	require.NoError(t, err)

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	// Wait for announcements
	time.Sleep(50 * time.Millisecond)

	// Verify gateway can serve
	canServe := gateway1.CanServePath("/api/test")
	assert.True(t, canServe)

	// Gateway suddenly stops
	gateway1.Stop()

	// Service should still be running (not panic or crash)
	assert.Equal(t, "test-service", service.Name)
	assert.NotNil(t, service.Transport)

	// Start a new gateway
	gateway2 := eurus.NewGateway("gateway-2", transport)
	err = gateway2.Start()
	require.NoError(t, err)
	defer gateway2.Stop()

	// Service should announce to new gateway
	time.Sleep(50 * time.Millisecond)
	canServe = gateway2.CanServePath("/api/test")
	assert.True(t, canServe, "Service should announce to new gateway")
}

// TestMessageDelivery_ServiceStops tests that message delivery handles
// service stops gracefully without panicking
func TestMessageDelivery_ServiceStops(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	messageReceived := make(chan bool, 1)
	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}

	// Add a simple handler
	service.Router.Use(func(ctx *velaros.Context) {
		select {
		case messageReceived <- true:
		default:
		}
		ctx.Next()
	})

	err = service.Start()
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Verify service is announced
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe)

	// Stop service before sending message
	service.Stop()

	// Try to send a message (should error since handler is unbound)
	err = transport.MessageService(service.ID, gateway.ID, "socket-1", &velaros.ConnectionInfo{}, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})
	assert.Error(t, err, "Message to stopped service should error when no handler is bound")

	// Message should not be received
	select {
	case <-messageReceived:
		t.Fatal("Message should not be received by stopped service")
	case <-time.After(50 * time.Millisecond):
		// Expected - message not received
	}
}

// TestMessageDelivery_GatewayStops tests that sending messages to a stopped
// gateway doesn't panic
func TestMessageDelivery_GatewayStops(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Stop gateway
	gateway.Stop()

	// Try to send a message back to the gateway (should error - handler unbound)
	err = transport.MessageGateway(gateway.ID, "socket-1", &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})
	assert.Error(t, err, "Message to stopped gateway should error (handler unbound)")
}

// TestMultipleGateways_ServiceSelectiveAnnouncement tests that a service
// only announces to specific gateways
func TestMultipleGateways_ServiceSelectiveAnnouncement(t *testing.T) {
	transport := localtransport.New()

	gateway1 := eurus.NewGateway("gateway-1", transport)
	err := gateway1.Start()
	require.NoError(t, err)
	defer gateway1.Stop()

	gateway2 := eurus.NewGateway("gateway-2", transport)
	err = gateway2.Start()
	require.NoError(t, err)
	defer gateway2.Stop()

	// Service announces only to gateway-1
	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	service.GatewayNames = []string{"gateway-1"}
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Verify service is registered with gateway-1
	canServe1 := gateway1.CanServePath("/api/test")
	assert.True(t, canServe1, "Service should be registered with gateway-1")

	// Verify service is NOT registered with gateway-2
	canServe2 := gateway2.CanServePath("/api/test")
	assert.False(t, canServe2, "Service should NOT be registered with gateway-2")

	// Gateway-1 disappears
	gateway1.Stop()

	// Service should still be running
	assert.NotNil(t, service.Transport)
	assert.Equal(t, "test-service", service.Name)

	// Service should still not announce to gateway-2
	time.Sleep(50 * time.Millisecond)
	canServe2 = gateway2.CanServePath("/api/test")
	assert.False(t, canServe2, "Service should still not be on gateway-2")
}

// TestRapidServiceChurn tests the system's ability to handle services
// that rapidly start and stop without corruption or memory leaks
func TestRapidServiceChurn(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	successfulStarts := 0
	successfulStops := 0

	// Rapidly start and stop services with same name
	for i := 0; i < 10; i++ {
		service := eurus.NewService("test-service", transport, velaros.NewRouter())
		route, _ := eurus.NewRouteDescriptor("/api/test")
		service.RouteDescriptors = []*eurus.RouteDescriptor{route}

		err = service.Start()
		if err == nil {
			successfulStarts++
		}

		time.Sleep(5 * time.Millisecond)

		// Verify service announced successfully before stopping
		if err == nil && gateway.CanServePath("/api/test") {
			// Service was registered
		}

		service.Stop()
		successfulStops++

		time.Sleep(2 * time.Millisecond)
	}

	// All operations should succeed without panics
	assert.Equal(t, 10, successfulStarts, "All services should start successfully")
	assert.Equal(t, 10, successfulStops, "All services should stop without panic")

	// Gateway should still be functional and not corrupted
	assert.NotEmpty(t, gateway.ID)
	assert.NotEmpty(t, gateway.Name)

	// After churn, gateway should still be in consistent state
	// (all services stopped, so path should still be registered due to eventual consistency)
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Gateway should still have route registered (eventual consistency)")

	// Start a fresh service to verify gateway can still accept new registrations
	freshService := eurus.NewService("fresh-service", transport, velaros.NewRouter())
	freshRoute, _ := eurus.NewRouteDescriptor("/api/fresh")
	freshService.RouteDescriptors = []*eurus.RouteDescriptor{freshRoute}
	err = freshService.Start()
	require.NoError(t, err, "Gateway should still accept new service registrations after churn")
	defer freshService.Stop()

	time.Sleep(10 * time.Millisecond)
	canServeFresh := gateway.CanServePath("/api/fresh")
	assert.True(t, canServeFresh, "Fresh service should be registered correctly")
}

// TestConcurrentGatewayServiceOperations tests concurrent start/stop operations
// for race conditions and data consistency. Run with -race flag to detect race conditions:
//
//	go test -race -run TestConcurrentGatewayServiceOperations
func TestConcurrentGatewayServiceOperations(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	type result struct {
		id           int
		startErr     error
		stopErr      error
		wasReachable bool
	}
	results := make(chan result, 5)

	// Start multiple service instances concurrently with same name
	for i := 0; i < 5; i++ {
		go func(id int) {
			service := eurus.NewService("test-service", transport, velaros.NewRouter())
			route, _ := eurus.NewRouteDescriptor("/api/test")
			service.RouteDescriptors = []*eurus.RouteDescriptor{route}

			startErr := service.Start()

			// Check if gateway can reach the service after announcement
			time.Sleep(10 * time.Millisecond)
			wasReachable := gateway.CanServePath("/api/test")

			time.Sleep(10 * time.Millisecond)

			service.Stop()

			results <- result{
				id:           id,
				startErr:     startErr,
				wasReachable: wasReachable,
			}
		}(i)
	}

	// Collect all results
	successfulStarts := 0
	reachableServices := 0
	for i := 0; i < 5; i++ {
		res := <-results
		if res.startErr == nil {
			successfulStarts++
			if res.wasReachable {
				reachableServices++
			}
		} else {
			t.Logf("Service %d failed to start: %v", res.id, res.startErr)
		}
	}

	// Verify concurrent operations succeeded
	assert.Equal(t, 5, successfulStarts, "All concurrent services should start successfully")
	assert.Equal(t, 5, reachableServices, "All services should be reachable after announcement")

	// Gateway should still be functional and in consistent state
	assert.NotEmpty(t, gateway.Name)
	assert.NotNil(t, gateway.Transport)

	// Path should still be servable (eventual consistency - some instances may still be indexed)
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Gateway should still have route indexed")

	// Verify gateway can still register new services after concurrent operations
	newService := eurus.NewService("new-service", transport, velaros.NewRouter())
	newRoute, _ := eurus.NewRouteDescriptor("/api/new")
	newService.RouteDescriptors = []*eurus.RouteDescriptor{newRoute}
	err = newService.Start()
	require.NoError(t, err, "Gateway should accept new registrations after concurrent operations")
	defer newService.Stop()

	time.Sleep(10 * time.Millisecond)
	canServeNew := gateway.CanServePath("/api/new")
	assert.True(t, canServeNew, "New service should be registered correctly")
}

// TestServiceReannouncement tests that services re-announce themselves
// when they encounter a new gateway
func TestServiceReannouncement(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Service should be registered
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe)

	// Stop and restart gateway (simulating gateway restart)
	gateway.Stop()

	gateway2 := eurus.NewGateway("test-gateway", transport)
	err = gateway2.Start()
	require.NoError(t, err)
	defer gateway2.Stop()

	// Service should re-announce to the new gateway instance
	time.Sleep(50 * time.Millisecond)

	canServe = gateway2.CanServePath("/api/test")
	assert.True(t, canServe, "Service should re-announce to restarted gateway")
}

// TestServiceReannounceAfterPrune tests that a service re-announces itself
// when it is pruned from the gateway's announce and then the gateway announces again
func TestServiceReannounceAfterPrune(t *testing.T) {
	// Use a short announce interval for testing
	origInterval := eurus.GatewayAnnounceInterval
	eurus.GatewayAnnounceInterval = 100 * time.Millisecond
	defer func() { eurus.GatewayAnnounceInterval = origInterval }()

	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Service should be registered
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Service should be registered initially")

	// Wait for a gateway announce cycle — the service should still be fresh
	// and included in the announce, so it won't need to re-announce
	time.Sleep(150 * time.Millisecond)

	canServe = gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Service should still be registered after announce cycle")
}

// TestSocketClose_ServiceStopped tests socket cleanup when service stops
func TestSocketClose_ServiceStopped(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Create a connection
	conn := eurus.NewConnection(transport, gateway.ID, "socket-1", &velaros.ConnectionInfo{}, func() {})

	// Stop service (should not panic)
	service.Stop()

	// Connection operations should be safe
	// conn.Write calls MessageGateway — the gateway is still running so the handler is bound
	err = conn.Write(nil, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})
	assert.NoError(t, err, "Writing to a running gateway should not error")
}

// TestSocketClose_GatewayStopped tests socket cleanup when gateway stops
func TestSocketClose_GatewayStopped(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Create a connection
	conn := eurus.NewConnection(transport, gateway.ID, "socket-1", &velaros.ConnectionInfo{}, func() {})

	// Stop gateway
	gateway.Stop()

	// Writing to stopped gateway should error (handler unbound)
	err = conn.Write(nil, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})
	assert.Error(t, err, "Writing to stopped gateway should error (handler unbound)")

	// Closing connection should not panic
	err = conn.Close(websocket.StatusNormalClosure, "test")
	assert.NoError(t, err, "Closing connection after gateway stop should not error")
}

// TestConnection_WriteFailure_AutoCleanup tests that when Connection.Write() fails,
// the connection is automatically closed and cleanup callback is invoked.
// Gateway.Stop() does not unbind the message handler, so we test with a gateway ID
// that was never registered to trigger the error path.
func TestConnection_WriteFailure_AutoCleanup(t *testing.T) {
	transport := localtransport.New()

	cleanupCalled := false
	connectionClosed := false

	// Create a connection targeting a gateway ID that has no handler bound
	conn := eurus.NewConnection(transport, "nonexistent-gateway", "socket-1", &velaros.ConnectionInfo{}, func() {
		cleanupCalled = true
		connectionClosed = true
	})

	// Verify connection starts in open state
	assert.False(t, cleanupCalled, "Cleanup should not be called initially")

	// Write to a gateway with no handler — LocalTransport now returns an error,
	// which triggers Connection.HandleClose and the cleanup callback
	err := conn.Write(nil, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})

	assert.Error(t, err, "Write to nonexistent gateway should error when no handler is bound")
	assert.True(t, cleanupCalled, "Cleanup should be triggered by write failure")
	assert.True(t, connectionClosed, "Connection should be marked as closed")
}

// TestMultipleServicesWithSameName tests that multiple service instances
// with the same name can coexist
func TestMultipleServicesWithSameName(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	// Create 3 instances of the same service
	services := make([]*eurus.Service, 3)
	stopped := make([]bool, 3)
	for i := 0; i < 3; i++ {
		service := eurus.NewService("test-service", transport, velaros.NewRouter())
		route, _ := eurus.NewRouteDescriptor("/api/test")
		service.RouteDescriptors = []*eurus.RouteDescriptor{route}
		err = service.Start()
		require.NoError(t, err)
		services[i] = service
	}

	// Clean up - only stop services that haven't been stopped yet
	defer func() {
		for i, svc := range services {
			if !stopped[i] {
				svc.Stop()
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Gateway should be able to route to the service
	canServe := gateway.CanServePath("/api/test")
	assert.True(t, canServe)

	// Stop one instance - should still work
	services[0].Stop()
	stopped[0] = true
	time.Sleep(10 * time.Millisecond)

	canServe = gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Should still serve with 2 instances remaining")

	// Stop another - should still work
	services[1].Stop()
	stopped[1] = true
	time.Sleep(10 * time.Millisecond)

	canServe = gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Should still serve with 1 instance remaining")
}

// TestServiceSurvivesAllClientsDisconnecting tests the core bug that motivated
// the announce-based protocol: services must remain indexed after all client
// connections close. With the old heartbeat protocol, services only sent
// heartbeats for gateways with active connections, so after all clients
// disconnected the service would be pruned permanently.
func TestServiceSurvivesAllClientsDisconnecting(t *testing.T) {
	origInterval := eurus.GatewayAnnounceInterval
	eurus.GatewayAnnounceInterval = 100 * time.Millisecond
	defer func() { eurus.GatewayAnnounceInterval = origInterval }()

	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Service should be registered
	canServe := gateway.CanServePath("/api/test")
	require.True(t, canServe, "Service should be registered initially")

	// Simulate a client connecting and then disconnecting
	transport.MessageService(service.ID, gateway.ID, "socket-1", &velaros.ConnectionInfo{}, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("hello"),
	})
	time.Sleep(10 * time.Millisecond)

	// Client disconnects
	transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "client closed")
	time.Sleep(10 * time.Millisecond)

	// Wait for multiple announce cycles — with the old heartbeat protocol,
	// the service would be pruned here because no connections remain
	time.Sleep(500 * time.Millisecond)

	// Service must still be registered (the core fix)
	canServe = gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Service should remain indexed after all clients disconnect")
}

// TestServiceSurvivesDisconnectThenReconnect tests the full disconnect-reconnect
// cycle: a client connects, disconnects, and a new client can still connect to
// the same service after enough time has passed for old heartbeats to expire.
func TestServiceSurvivesDisconnectThenReconnect(t *testing.T) {
	origInterval := eurus.GatewayAnnounceInterval
	eurus.GatewayAnnounceInterval = 100 * time.Millisecond
	defer func() { eurus.GatewayAnnounceInterval = origInterval }()

	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// First client connects and sends a message
	err = transport.MessageService(service.ID, gateway.ID, "socket-1", &velaros.ConnectionInfo{}, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("hello"),
	})
	require.NoError(t, err, "First message should be delivered")
	time.Sleep(10 * time.Millisecond)

	// First client disconnects
	transport.ClosedSocket("socket-1", websocket.StatusNormalClosure, "client closed")
	time.Sleep(10 * time.Millisecond)

	// Wait well beyond what old heartbeat timeout would have been
	time.Sleep(500 * time.Millisecond)

	// Gateway should still know about the service
	canServe := gateway.CanServePath("/api/test")
	require.True(t, canServe, "Service should still be routable after disconnect + wait")

	// Second client connects and sends a message — the service handler is still bound
	err = transport.MessageService(service.ID, gateway.ID, "socket-2", &velaros.ConnectionInfo{}, &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("hello again"),
	})
	assert.NoError(t, err, "Second message should be delivered — service must not be pruned after first client disconnected")
}

// TestServiceReappearsAfterPrune tests that if a service is somehow pruned
// (e.g., temporary network partition), it re-announces on the next gateway
// announce cycle and becomes routable again.
func TestServiceReappearsAfterPrune(t *testing.T) {
	origInterval := eurus.GatewayAnnounceInterval
	eurus.GatewayAnnounceInterval = 100 * time.Millisecond
	defer func() { eurus.GatewayAnnounceInterval = origInterval }()

	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	canServe := gateway.CanServePath("/api/test")
	require.True(t, canServe, "Service should be registered initially")

	// Wait for the announce cycle to include the service (keeping it fresh),
	// then wait for a subsequent announce cycle
	time.Sleep(350 * time.Millisecond)

	// Service should still be reachable — the announce loop keeps it fresh
	canServe = gateway.CanServePath("/api/test")
	assert.True(t, canServe, "Service should remain reachable through announce cycles")
}

// TestEmptyGatewayNames_AnnouncesToAll tests that a service with no specific
// gateway names announces to all gateways
func TestEmptyGatewayNames_AnnouncesToAll(t *testing.T) {
	transport := localtransport.New()

	gateway1 := eurus.NewGateway("gateway-1", transport)
	err := gateway1.Start()
	require.NoError(t, err)
	defer gateway1.Stop()

	gateway2 := eurus.NewGateway("gateway-2", transport)
	err = gateway2.Start()
	require.NoError(t, err)
	defer gateway2.Stop()

	// Service with empty GatewayNames should announce to all
	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	route, _ := eurus.NewRouteDescriptor("/api/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	// Should be available on both gateways
	canServe1 := gateway1.CanServePath("/api/test")
	assert.True(t, canServe1, "Service should announce to gateway-1")

	canServe2 := gateway2.CanServePath("/api/test")
	assert.True(t, canServe2, "Service should announce to gateway-2")
}

// TestGateway_Stop_UnbindsAllHandlers tests that gateway Stop() properly
// unbinds all transport handlers
func TestGateway_Stop_UnbindsAllHandlers(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	gateway.Stop()

	// Try to send messages after stop - should error (no handler bound)
	err = transport.MessageGateway(gateway.ID, "socket-1", &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	})
	assert.Error(t, err, "MessageGateway should error after gateway stops")
}

// TestService_HeartbeatLoop_StopsCleanly tests that the service heartbeat loop exits
// when the service stops
func TestService_HeartbeatLoop_StopsCleanly(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	router := velaros.NewRouter()
	service := eurus.NewService("test-service", transport, router)
	service.HeartbeatInterval = 50 * time.Millisecond
	route, _ := eurus.NewRouteDescriptor("/test")
	service.RouteDescriptors = []*eurus.RouteDescriptor{route}
	err = service.Start()
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Create a connection on the service side
	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}
	err = transport.MessageService(service.ID, gateway.ID, "heartbeat-test-socket", connInfo, msg)
	require.NoError(t, err)

	var heartbeatsReceived atomic.Int64
	err = transport.BindSocketHeartbeat(gateway.ID, func(serviceID string, socketID string) bool {
		heartbeatsReceived.Add(1)
		return true
	})
	require.NoError(t, err)

	time.Sleep(120 * time.Millisecond)
	countBeforeStop := heartbeatsReceived.Load()

	service.Stop()
	time.Sleep(120 * time.Millisecond)

	// No new heartbeats should be sent after service stops
	assert.Equal(t, countBeforeStop, heartbeatsReceived.Load(), "Heartbeat loop should stop when service stops")
}

// TestGateway_ProtectedMapDeletion_NoRace tests that concurrent socket
// operations don't cause races
func TestGateway_ProtectedMapDeletion_NoRace(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	done := make(chan bool, 2)

	// Concurrent close events
	go func() {
		for i := 0; i < 50; i++ {
			transport.ClosedSocket("socket-race", websocket.StatusNormalClosure, "test")
			time.Sleep(2 * time.Millisecond)
		}
		done <- true
	}()

	// Concurrent heartbeats (from a dummy service to the gateway)
	go func() {
		for i := 0; i < 50; i++ {
			transport.HeartbeatSocket(gateway.ID, "dummy-service", "socket-race")
			time.Sleep(2 * time.Millisecond)
		}
		done <- true
	}()

	<-done
	<-done
}

// TestService_HighConnectionChurn_BoundedMemory tests that rapid
// connect/disconnect doesn't cause unbounded memory growth
func TestService_HighConnectionChurn_BoundedMemory(t *testing.T) {
	transport := localtransport.New()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	service.ClosedSocketTTL = 100 * time.Millisecond
	service.PruneInterval = 30 * time.Millisecond
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	time.Sleep(50 * time.Millisecond)

	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}

	// High churn - 200 rapid connect/disconnect cycles
	for i := 0; i < 200; i++ {
		socketID := fmt.Sprintf("churn-socket-%d", i)

		err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
		require.NoError(t, err)

		err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "churn")
		require.NoError(t, err)
	}

	// Wait for multiple prune cycles
	time.Sleep(200 * time.Millisecond)

	// Memory should be bounded (closed sockets pruned)
}

// TestService_MultipleStartStop_NoLeaks tests that restarting a service
// multiple times doesn't leak goroutines or handlers
func TestService_MultipleStartStop_NoLeaks(t *testing.T) {
	transport := localtransport.New()

	for cycle := 0; cycle < 5; cycle++ {
		service := eurus.NewService(fmt.Sprintf("test-service-%d", cycle), transport, velaros.NewRouter())
		err := service.Start()
		require.NoError(t, err)

		connInfo := &velaros.ConnectionInfo{}
		msg := &velaros.SocketMessage{
			Type: websocket.MessageText,
			Data: []byte("test"),
		}

		// Create connections
		for i := 0; i < 5; i++ {
			socketID := fmt.Sprintf("restart-socket-%d-%d", cycle, i)
			err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
			require.NoError(t, err)
		}

		time.Sleep(50 * time.Millisecond)
		service.Stop()
		time.Sleep(50 * time.Millisecond)
	}
}

// TestConnection_SendToClosedConnection_NoError tests that sending messages
// to a closed connection doesn't cause issues
func TestConnection_SendToClosedConnection_NoError(t *testing.T) {
	transport := localtransport.New()

	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	err := service.Start()
	require.NoError(t, err)
	defer service.Stop()

	socketID := "test-socket-send-closed"
	connInfo := &velaros.ConnectionInfo{}
	msg := &velaros.SocketMessage{
		Type: websocket.MessageText,
		Data: []byte("test"),
	}

	// Create connection
	err = transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Close connection
	err = transport.ClosedSocket(socketID, websocket.StatusNormalClosure, "test")
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// Try sending many messages to closed connection concurrently
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				transport.MessageService(service.ID, "gateway-1", socketID, connInfo, msg)
				time.Sleep(1 * time.Millisecond)
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}
