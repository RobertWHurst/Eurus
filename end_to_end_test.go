package eurus_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RobertWHurst/velaros"
	"github.com/RobertWHurst/velaros/middleware/json"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/telemetrytv/eurus"
	"github.com/telemetrytv/eurus/transport/localtransport"
)

// TestEndToEnd_WebSocketClientToService tests the complete flow:
// WebSocket Client -> Gateway -> Service (5 instances) -> Handler -> Response back
// Verifies that socket-level storage pins requests to the same service instance
func TestEndToEnd_WebSocketClientToService(t *testing.T) {
	transport := localtransport.New()

	// Create gateway
	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	// Create a Velaros router for HTTP, add JSON middleware and gateway
	router := velaros.NewRouter()
	router.Use(json.Middleware())
	router.Use(gateway)

	// Start HTTP test server with the router
	server := httptest.NewServer(router)
	defer server.Close()

	// Create service with 5 instances
	var services []*eurus.Service
	var handlerCallCounts sync.Map // Track which instances handle requests

	route, err := eurus.NewRouteDescriptor("/api/*")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		// Create a router with JSON middleware and handlers
		serviceRouter := velaros.NewRouter()
		serviceRouter.Use(json.Middleware())

		service := eurus.NewService("test-service", transport, serviceRouter)
		service.RouteDescriptors = []*eurus.RouteDescriptor{route}

		instanceID := service.ID

		// Handler for /api/set - stores value on socket
		serviceRouter.Bind("/api/set", func(ctx *velaros.Context) {
			t.Logf("[Instance %s] /api/set handler called", instanceID[:8])
			// Track that this instance handled a request
			count, _ := handlerCallCounts.LoadOrStore(instanceID, 0)
			handlerCallCounts.Store(instanceID, count.(int)+1)

			// Parse request
			var req struct {
				Value string `json:"value"`
			}
			err := ctx.Unmarshal(&req)
			if err != nil {
				ctx.Reply(map[string]string{"error": "invalid request"})
				return
			}

			// Store value on socket (per-connection storage)
			ctx.SetOnSocket("stored_value", req.Value)
			ctx.SetOnSocket("instance_id", instanceID)

			// Reply with success
			ctx.Reply(map[string]interface{}{
				"status":      "ok",
				"instance_id": instanceID,
			})
		})

		// Handler for /api/get - retrieves value from socket
		serviceRouter.Bind("/api/get", func(ctx *velaros.Context) {
			// Track that this instance handled a request
			count, _ := handlerCallCounts.LoadOrStore(instanceID, 0)
			handlerCallCounts.Store(instanceID, count.(int)+1)

			// Retrieve value from socket storage
			value, ok := ctx.GetFromSocket("stored_value")
			if !ok {
				ctx.Reply(map[string]string{"error": "no value stored"})
				return
			}

			storedInstanceID, _ := ctx.GetFromSocket("instance_id")

			// Reply with retrieved value
			ctx.Reply(map[string]interface{}{
				"value":       value,
				"instance_id": storedInstanceID,
			})
		})

		err = service.Start()
		require.NoError(t, err)
		services = append(services, service)
		defer service.Stop()
	}

	// Verify gateway can serve the path
	canServe := gateway.CanServePath("/api/set")
	require.True(t, canServe, "Gateway should be able to route to /api/set")

	t.Logf("Gateway is ready, %d services registered", len(services))

	// Connect WebSocket client to gateway
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "test complete")

	// Test 1: Send "set" request
	setRequest := map[string]interface{}{
		"id":    "msg-1",
		"path":  "/api/set",
		"value": "test123",
	}

	t.Logf("Sending set request: %+v", setRequest)
	err = wsjson.Write(ctx, conn, setRequest)
	require.NoError(t, err)

	t.Log("Waiting for set response...")
	// Read "set" response
	var setResponse map[string]interface{}
	err = wsjson.Read(ctx, conn, &setResponse)
	require.NoError(t, err)
	t.Logf("Received set response: %+v", setResponse)

	// Extract data from response
	setData, ok := setResponse["data"].(map[string]interface{})
	require.True(t, ok, "Response should have data field")

	// Verify response
	assert.Equal(t, "ok", setData["status"])
	assert.NotEmpty(t, setData["instance_id"])
	firstInstanceID := setData["instance_id"].(string)

	t.Logf("First request handled by instance: %s", firstInstanceID)

	// Test 2: Send "get" request (should go to same instance due to socket pinning)
	getRequest := map[string]interface{}{
		"id":   "msg-2",
		"path": "/api/get",
	}

	err = wsjson.Write(ctx, conn, getRequest)
	require.NoError(t, err)

	// Read "get" response
	var getResponse map[string]interface{}
	err = wsjson.Read(ctx, conn, &getResponse)
	require.NoError(t, err)

	// Extract data from response
	getData, ok := getResponse["data"].(map[string]interface{})
	require.True(t, ok, "Response should have data field")

	// Verify response
	assert.Equal(t, "test123", getData["value"])
	assert.Equal(t, firstInstanceID, getData["instance_id"])

	t.Logf("Second request handled by instance: %s", getData["instance_id"])

	// Verify that only ONE service instance handled both requests
	activeInstanceCount := 0
	handlerCallCounts.Range(func(key, value interface{}) bool {
		count := value.(int)
		if count > 0 {
			activeInstanceCount++
			t.Logf("Instance %s handled %d requests", key, count)
		}
		return true
	})

	assert.Equal(t, 1, activeInstanceCount, "Only one service instance should have handled requests (socket pinning)")

	// Verify that the instance that handled requests got both calls
	count, ok := handlerCallCounts.Load(firstInstanceID)
	require.True(t, ok, "Should have call count for the active instance")
	assert.Equal(t, 2, count.(int), "The active instance should have handled both requests")
}

// TestEndToEnd_MultipleConnections tests load balancing across service instances
// Verifies that different connections get distributed to different service instances
func TestEndToEnd_MultipleConnections(t *testing.T) {
	transport := localtransport.New()

	// Create gateway
	gateway := eurus.NewGateway("test-gateway", transport)
	err := gateway.Start()
	require.NoError(t, err)
	defer gateway.Stop()

	// Create a Velaros router for HTTP, add JSON middleware and gateway
	router := velaros.NewRouter()
	router.Use(json.Middleware())
	router.Use(gateway)

	// Start HTTP test server with the router
	server := httptest.NewServer(router)
	defer server.Close()

	// Create service with 5 instances
	var services []*eurus.Service
	var handlerCallCounts sync.Map // Track which instances handle requests

	route, err := eurus.NewRouteDescriptor("/api/*")
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		serviceRouter := velaros.NewRouter()
		serviceRouter.Use(json.Middleware())

		service := eurus.NewService("test-service", transport, serviceRouter)
		service.RouteDescriptors = []*eurus.RouteDescriptor{route}

		instanceID := service.ID

		// Simple handler that returns instance ID
		serviceRouter.Bind("/api/ping", func(ctx *velaros.Context) {
			count, _ := handlerCallCounts.LoadOrStore(instanceID, 0)
			handlerCallCounts.Store(instanceID, count.(int)+1)

			ctx.Reply(map[string]interface{}{
				"instance_id": instanceID,
			})
		})

		err = service.Start()
		require.NoError(t, err)
		services = append(services, service)
		defer service.Stop()
	}

	// Give services time to announce themselves
	time.Sleep(10 * time.Millisecond)

	// Create 10 WebSocket connections and make one request from each
	// Keep connections open to test load balancing
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	instancesSeen := make(map[string]bool)
	var connections []*websocket.Conn

	// Open all connections first (while keeping them open)
	for i := 0; i < 10; i++ {
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		require.NoError(t, err)
		connections = append(connections, conn)

		// Send ping request
		pingRequest := map[string]interface{}{
			"id":   fmt.Sprintf("ping-%d", i),
			"path": "/api/ping",
		}

		err = wsjson.Write(ctx, conn, pingRequest)
		require.NoError(t, err)

		// Read response
		var response map[string]interface{}
		err = wsjson.Read(ctx, conn, &response)
		require.NoError(t, err)

		// Extract instance ID
		data, ok := response["data"].(map[string]interface{})
		require.True(t, ok, "Response should have data field")
		instanceID := data["instance_id"].(string)
		instancesSeen[instanceID] = true

		t.Logf("Connection %d routed to instance: %s", i, instanceID[:8])
	}

	// Close all connections after testing
	for _, conn := range connections {
		conn.Close(websocket.StatusNormalClosure, "test complete")
	}

	// Verify that multiple instances were used (load balancing)
	t.Logf("Total unique instances used: %d", len(instancesSeen))
	assert.GreaterOrEqual(t, len(instancesSeen), 2, "Multiple connections should be distributed across multiple service instances")

	// Log instance usage
	handlerCallCounts.Range(func(key, value interface{}) bool {
		count := value.(int)
		if count > 0 {
			t.Logf("Instance %s handled %d requests", key, count)
		}
		return true
	})
}
