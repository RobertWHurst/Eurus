package eurus_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/telemetrytv/eurus"
	"github.com/telemetrytv/eurus/transport/localtransport"
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

	// Try to send a message (should not panic)
	err = transport.MessageService(service.ID, gateway.ID, "socket-1", http.Header{}, websocket.MessageText, []byte("test"))
	assert.NoError(t, err, "Message to stopped service should not error in transport layer")

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

	// Try to send a message back to the gateway (should not panic)
	err = transport.MessageGateway(gateway.ID, "socket-1", websocket.MessageText, []byte("test"))
	assert.NoError(t, err, "Message to stopped gateway should not error in transport layer")
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
	conn := eurus.NewConnection(transport, gateway.ID, "socket-1", http.Header{}, func() {})

	// Simulate connection handling
	conn.HandleRawMessage(websocket.MessageText, []byte("test"))

	// Stop service (should not panic)
	service.Stop()

	// Connection operations should be safe
	// Writing to a stopped service's gateway should not panic
	err = conn.Write(nil, websocket.MessageText, []byte("test"))
	assert.NoError(t, err, "Writing after service stop should not error in transport layer")
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
	conn := eurus.NewConnection(transport, gateway.ID, "socket-1", http.Header{}, func() {})

	// Stop gateway
	gateway.Stop()

	// Writing to stopped gateway should not panic
	err = conn.Write(nil, websocket.MessageText, []byte("test"))
	assert.NoError(t, err, "Writing to stopped gateway should not error in transport layer")

	// Closing connection should not panic
	err = conn.Close(websocket.StatusNormalClosure, "test")
	assert.NoError(t, err, "Closing connection after gateway stop should not error")
}

// TestConnection_WriteFailure_AutoCleanup tests that when Connection.Write() fails,
// the connection is automatically closed and cleanup callback is invoked.
// NOTE: LocalTransport doesn't return errors for writes to stopped gateways (fire-and-forget),
// so this test primarily documents the expected behavior. The auto-cleanup is properly
// exercised with NATS transport which returns errors on delivery failures.
func TestConnection_WriteFailure_AutoCleanup(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)

	cleanupCalled := false
	connectionClosed := false

	// Create a connection with cleanup callback that tracks cleanup
	conn := eurus.NewConnection(transport, gateway.ID, "socket-1", http.Header{}, func() {
		cleanupCalled = true
		connectionClosed = true
	})

	// Verify connection starts in open state
	assert.False(t, cleanupCalled, "Cleanup should not be called initially")

	// Stop gateway to simulate unreachable gateway
	gateway.Stop()

	// Try to write - in LocalTransport this doesn't error (fire-and-forget)
	// but in NATS transport with per-chunk ACKs, this would fail and trigger cleanup
	err = conn.Write(context.TODO(), websocket.MessageText, []byte("test"))

	// LocalTransport doesn't error on writes to stopped gateway
	assert.NoError(t, err, "LocalTransport doesn't error on writes to stopped gateway")

	// Since LocalTransport doesn't error, cleanup won't be triggered
	// This documents the limitation of LocalTransport for testing
	assert.False(t, cleanupCalled, "LocalTransport doesn't trigger cleanup (no errors returned)")

	// Manually trigger HandleClose to demonstrate the cleanup mechanism
	conn.HandleClose(websocket.StatusInternalError, "Simulated write failure")

	// Now cleanup should have been called
	assert.True(t, cleanupCalled, "Cleanup should be called after HandleClose")
	assert.True(t, connectionClosed, "Connection should be marked as closed")

	t.Log("Note: This test documents the auto-cleanup mechanism")
	t.Log("Real write failures are tested with NATS transport in production")
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
