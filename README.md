# Eurus

A WebSocket API gateway for microservices. Routes messages to services based on path patterns.

Eurus lets you distribute WebSocket connections across multiple backend services. Instead of all connections going to one server, a gateway routes messages to different services based on the message path - similar to how HTTP API gateways work. Each connection gets pinned to a service instance that maintains its state. Services are [Velaros](https://github.com/RobertWHurst/Velaros) routers, so you get bidirectional communication, pattern-based routing, and connection state management.

[![Go Reference](https://pkg.go.dev/badge/github.com/RobertWHurst/eurus.svg)](https://pkg.go.dev/github.com/RobertWHurst/eurus)
[![Go Report Card](https://goreportcard.com/badge/github.com/RobertWHurst/eurus)](https://goreportcard.com/report/github.com/RobertWHurst/eurus)
[![GitHub release](https://img.shields.io/github/v/release/RobertWHurst/eurus)](https://github.com/RobertWHurst/eurus/releases)
[![License](https://img.shields.io/github/license/RobertWHurst/eurus)](LICENSE)
[![Sponsor](https://img.shields.io/static/v1?label=Sponsor&message=%E2%9D%A4&logo=GitHub&color=%23fe8e86)](https://github.com/sponsors/RobertWHurst)

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
  - [Gateway](#gateway)
  - [Service](#service)
  - [Client](#client)
- [Core Concepts](#core-concepts)
  - [Gateway](#gateway-1)
  - [Service](#service-1)
  - [Socket Pinning](#socket-pinning)
  - [Service Discovery](#service-discovery)
  - [Encoding Middleware](#encoding-middleware)
- [Creating a Gateway](#creating-a-gateway)
  - [Basic Setup](#basic-setup)
  - [Multiple Gateways](#multiple-gateways)
- [Creating Services](#creating-services)
  - [Basic Service](#basic-service)
  - [Scaling Services](#scaling-services)
- [Transports](#transports)
  - [Local Transport](#local-transport)
  - [NATS Transport](#nats-transport)
- [Architecture](#architecture)
  - [Message Flow](#message-flow)
  - [Load Balancing](#load-balancing)
  - [Failure Handling](#failure-handling)
- [Advanced Usage](#advanced-usage)
  - [Selective Gateway Routing](#selective-gateway-routing)
  - [Connection Lifecycle](#connection-lifecycle)
  - [Bidirectional Communication](#bidirectional-communication)
- [Testing](#testing)
- [Comparison with Zephyr](#comparison-with-zephyr)
- [Production Considerations](#production-considerations)
  - [Observability](#observability)
  - [Health Monitoring](#health-monitoring)
- [Help Welcome](#help-welcome)
- [License](#license)
- [Related Projects](#related-projects)

## Features

- 🌐 **WebSocket Gateway** - Route messages to backend services transparently
- 🔍 **Service Discovery** - Services announce routes automatically on startup
- 📌 **Socket Pinning** - Each connection stays with one service instance
- ⚖️ **Load Balancing** - Least-connections distribution for new connections
- 🔒 **Public/Private Routes** - `PublicBind()` for external, `Bind()` for internal
- 🔌 **Pluggable Transports** - Local for development, NATS for production
- 🚀 **Horizontal Scaling** - Run multiple instances seamlessly
- 🎯 **Velaros Native** - Services are standard Velaros routers
- ⚡ **High Performance** - Minimal routing overhead
- 🧪 **Production Ready** - Race-tested with comprehensive test coverage

## Installation

```bash
go get github.com/RobertWHurst/eurus
```

For production with NATS:
```bash
go get github.com/nats-io/nats.go
```

## Quick Start

Here's a minimal chat application showing how Eurus distributes WebSocket connections across microservices. The gateway receives WebSocket connections from clients and routes messages to backend services based on the message path.

**Important:** Gateway and services must use the same encoding middleware (JSON, MessagePack, or Protobuf).

### Gateway

The gateway accepts WebSocket connections and routes messages to services:

```go
package main

import (
    "net/http"
    "github.com/RobertWHurst/velaros"
    "github.com/RobertWHurst/velaros/middleware/json"
    "github.com/RobertWHurst/eurus"
    "github.com/RobertWHurst/eurus/transport/localtransport"
)

func main() {
    // Use local transport for development (NATS for production)
    transport := localtransport.New()

    // Start the gateway
    gateway := eurus.NewGateway("main-gateway", transport)
    gateway.Start()
    defer gateway.Stop()

    // Mount gateway on Velaros router - this is where the magic happens
    router := velaros.NewRouter()
    router.Use(json.Middleware())  // Must match what services use
    router.Use(gateway)             // Gateway becomes middleware

    // Serve WebSocket connections on /ws
    http.Handle("/ws", router)
    http.ListenAndServe(":8080", nil)
}
```

### Service

Services handle messages using standard Velaros routers:

```go
package main

import (
    "log"
    "github.com/RobertWHurst/velaros"
    "github.com/RobertWHurst/velaros/middleware/json"
    "github.com/RobertWHurst/eurus"
    "github.com/RobertWHurst/eurus/transport/localtransport"
)

func main() {
    router := velaros.NewRouter()
    router.Use(json.Middleware())

    // PublicBind exposes routes through the gateway
    router.PublicBind("/chat/join", func(ctx *velaros.Context) {
        var req struct {
            Username string `json:"username"`
            Room     string `json:"room"`
        }
        ctx.Unmarshal(&req)

        // Socket storage persists for the entire connection
        ctx.SetOnSocket("username", req.Username)
        ctx.SetOnSocket("room", req.Room)

        log.Printf("User %s joined room %s", req.Username, req.Room)
        ctx.Reply(map[string]string{
            "status": "joined",
            "room":   req.Room,
        })
    })

    router.PublicBind("/chat/message", func(ctx *velaros.Context) {
        var msg struct {
            Text string `json:"text"`
        }
        ctx.Unmarshal(&msg)

        // Retrieve stored user info
        username := ctx.MustGetFromSocket("username").(string)
        room := ctx.MustGetFromSocket("room").(string)

        log.Printf("[%s] %s: %s", room, username, msg.Text)

        // In production, broadcast to room members
        ctx.Reply(map[string]string{"status": "sent"})
    })

    // Regular Bind() creates internal-only routes
    router.Bind("/health", func(ctx *velaros.Context) {
        ctx.Reply(map[string]string{"status": "healthy"})
    })

    // Connect to same transport as gateway
    transport := localtransport.New()
    service := eurus.NewService("chat-service", transport, router)

    log.Println("Chat service starting...")
    service.Run()  // Announces routes and blocks
}
```

### Client

JavaScript client connecting through the gateway:

```javascript
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = () => {
    // Join a chat room
    ws.send(JSON.stringify({
        path: '/chat/join',
        id: 'msg-1',
        data: {
            username: 'Alice',
            room: 'general'
        }
    }));
};

ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);

    if (msg.id === 'msg-1' && msg.data.status === 'joined') {
        // Now we can send messages
        ws.send(JSON.stringify({
            path: '/chat/message',
            id: 'msg-2',
            data: { text: 'Hello everyone!' }
        }));
    }
};
```

**Note:** Messages must include `path` for routing and `id` for request/response correlation. The gateway uses the path to route to the correct service, just like HTTP routing.

## Core Concepts

### Gateway

The gateway is middleware that routes WebSocket messages to your backend services. When a client sends a message, the gateway looks at the path and forwards it to the appropriate service - just like an HTTP reverse proxy, but for WebSocket.

What makes this powerful is that the gateway maintains a "virtual connection" to the service. The client has one WebSocket connection to the gateway, and the gateway manages the routing to services transparently. This abstraction is what enables horizontal scaling for WebSocket.

### Service

Services are just Velaros routers that handle messages. The key innovation is the distinction between public and private routes:

- **`PublicBind()`** - Routes accessible through the gateway (announced to gateways)
- **`Bind()`** - Internal routes NOT accessible through gateway (kept private)

This separation lets you expose customer-facing APIs while keeping health checks, metrics, and admin endpoints private. It's the same pattern as having public and private methods in a class, but for network services.

### Socket Pinning

Here's the crucial part: when a client connects and sends its first message, the gateway "pins" that connection to a specific service instance. Every message from that client will go to the same instance until the connection closes.

Why does this matter? Because it lets you store state on the connection - user sessions, authentication, conversation context - and know it will be there for every message. Without pinning, each message could hit a different instance and you'd lose all your state.

```go
// First message: authenticate and store state
router.PublicBind("/auth/login", func(ctx *velaros.Context) {
    // Store on socket - persists for connection lifetime
    ctx.SetOnSocket("userID", authenticatedUserID)
    ctx.SetOnSocket("role", userRole)
})

// Subsequent messages: access stored state
router.PublicBind("/api/data", func(ctx *velaros.Context) {
    userID := ctx.MustGetFromSocket("userID").(string)
    role := ctx.MustGetFromSocket("role").(string)
    // User state available for all messages from this connection
})
```

### Service Discovery

Service discovery happens automatically. When you start a service, it announces its public routes to all gateways. When you stop a service, the gateway discovers this on the next failed message delivery and removes it from routing.

This is intentionally simple - no complex consensus protocols or leader election. Services announce themselves, gateways track them, and failed deliveries trigger cleanup. It's eventual consistency, which works perfectly for most applications.

### Encoding Middleware

**Important:** The gateway and all services must use the same encoding middleware. This is how the gateway extracts the message path for routing.

```go
// Gateway and services must match
router.Use(json.Middleware())     // JSON encoding
// OR
router.Use(msgpack.Middleware())  // MessagePack encoding
// OR
router.Use(protobuf.Middleware()) // Protocol Buffers
```

Pick one encoding for your entire system. JSON is great for development, MessagePack for performance, and Protocol Buffers when you need schema validation.

## Creating a Gateway

Setting up a gateway is simple - give it a name, a transport, and mount it on a Velaros router:

### Basic Setup

```go
import (
    "net/http"
    "github.com/RobertWHurst/velaros"
    "github.com/RobertWHurst/velaros/middleware/json"
    "github.com/nats-io/nats.go"
    "github.com/RobertWHurst/eurus"
    "github.com/RobertWHurst/eurus/transport/natstransport"
)

// Connect to NATS
nc, _ := nats.Connect("nats://localhost:4222")
transport := natstransport.New(nc)

// Create gateway
gateway := eurus.NewGateway("api-gateway", transport)
gateway.Start()
defer gateway.Stop()

// Mount on router with encoding
router := velaros.NewRouter()
router.Use(json.Middleware())  // Must match service encoding
router.Use(gateway)

// Start accepting WebSocket connections
http.Handle("/ws", router)
http.ListenAndServe(":8080", nil)
```

### Multiple Gateways

Run multiple gateways for high availability. Services announce to all gateways automatically:

```go
// Gateway 1 - Port 8080
gateway1 := eurus.NewGateway("api-gateway", transport1)
router1 := velaros.NewRouter()
router1.Use(json.Middleware())
router1.Use(gateway1)
go http.ListenAndServe(":8080", router1)

// Gateway 2 - Port 8081
gateway2 := eurus.NewGateway("api-gateway", transport2)
router2 := velaros.NewRouter()
router2.Use(json.Middleware())
router2.Use(gateway2)
go http.ListenAndServe(":8081", router2)
```

## Creating Services

Services are just Velaros routers. The key is using `PublicBind()` for routes you want accessible through the gateway:

### Basic Service

```go
import (
    "github.com/RobertWHurst/velaros"
    "github.com/RobertWHurst/velaros/middleware/json"
    "github.com/RobertWHurst/eurus"
    "github.com/RobertWHurst/eurus/transport/natstransport"
)

// Create router with handlers
router := velaros.NewRouter()
router.Use(json.Middleware())  // MUST match gateway encoding

// Public routes - accessible via gateway
router.PublicBind("/api/users/:id", func(ctx *velaros.Context) {
    userID := ctx.Params().Get("id")
    user := getUserByID(userID)
    ctx.Reply(user)
})

router.PublicBind("/api/posts/**", func(ctx *velaros.Context) {
    // Wildcard captures rest of path
    path := ctx.Path()  // e.g., "/api/posts/2024/11/my-post"
    ctx.Reply(getPost(path))
})

// Private routes - NOT accessible via gateway
router.Bind("/health", func(ctx *velaros.Context) {
    ctx.Reply(map[string]string{"status": "healthy"})
})

router.Bind("/metrics", func(ctx *velaros.Context) {
    ctx.Reply(getMetrics())
})

// Create and start service
nc, _ := nats.Connect("nats://localhost:4222")
transport := natstransport.New(nc)
service := eurus.NewService("api-service", transport, router)

// Optional: Target specific gateways
service.GatewayNames = []string{"public-gateway"}  // Default: all gateways

// Start service
service.Run()  // Blocks until stopped
```

### Scaling Services

Run multiple instances for load distribution:

```bash
# Terminal 1
go run service.go  # Instance with auto-generated ID

# Terminal 2
go run service.go  # Another instance

# Terminal 3
go run service.go  # Third instance
```

The gateway distributes new connections using least-connections load balancing. Each connection stays pinned to its assigned instance.

## Transports

Transports handle communication between gateways and services. You have two options:

### Local Transport

Perfect for development and testing - everything runs in a single process:

```go
import "github.com/RobertWHurst/eurus/transport/localtransport"

transport := localtransport.New()
// Share this instance between gateway and services
```

Use this for:
- Local development
- Unit tests
- Simple applications that don't need distribution
- Getting started quickly without NATS

### NATS Transport

For production systems where gateways and services run on different machines:

```go
import (
    "github.com/nats-io/nats.go"
    "github.com/RobertWHurst/eurus/transport/natstransport"
)

nc, _ := nats.Connect("nats://nats-server:4222")
transport := natstransport.New(nc)
```

NATS gives you:
- True distribution across machines
- Automatic reconnection and failover
- High availability with clustering
- Battle-tested messaging infrastructure

Quick NATS setup:
```bash
# Development
docker run -p 4222:4222 nats:latest

# Production cluster
nats-server --cluster nats://0.0.0.0:6222 \
            --routes nats://node1:6222,nats://node2:6222
```

## Architecture

### Message Flow

Here's what happens when a client sends a message:

1. Client connects to `/ws` endpoint
2. Velaros accepts the WebSocket connection
3. Client sends: `{path: "/chat/join", id: "1", data: {...}}`
4. Velaros decodes the message and extracts the path
5. Gateway middleware sees "/chat/join" and finds a service that handles it
6. Gateway creates a virtual connection to that service instance (first message only)
7. Service receives the message and handles it with the matching route
8. Service sends response back through gateway to client

The beauty is that the client just sees a single WebSocket connection, while behind the scenes you can have dozens of service instances handling different routes.

### Load Balancing

The gateway uses least-connections load balancing - new connections go to the instance with the fewest active connections. Simple and effective:

```
Instance A: 5 connections
Instance B: 3 connections  ← New connection goes here
Instance C: 4 connections
```

As connections close, the distribution automatically rebalances. No configuration needed, it just works.

### Failure Handling

Eurus keeps things simple with eventual consistency:

- Service crashes? The gateway discovers this on the next message delivery and removes it
- Gateway crashes? Clients reconnect to another gateway instance
- Network issues? NATS handles reconnection automatically

For production, you'll want health checks to detect failures faster:

```go
// Check service health periodically
ticker := time.NewTicker(30 * time.Second)
for range ticker.C {
    for _, service := range services {
        if !pingService(service) {
            gateway.RemoveService(service)
        }
    }
}
```

But honestly? The default behavior works fine for most applications. Services fail, gateways detect it, clients reconnect. Simple.

## Advanced Usage

### Selective Gateway Routing

Services can target specific gateways for multi-tenant or security-zoned deployments:

```go
// Public-facing service
publicService := eurus.NewService("api", transport, publicRouter)
publicService.GatewayNames = []string{"public-gateway"}
publicService.Start()

// Internal admin service
adminService := eurus.NewService("admin", transport, adminRouter)
adminService.GatewayNames = []string{"internal-gateway"}
adminService.Start()

// Service accessible from both
sharedService := eurus.NewService("shared", transport, sharedRouter)
// Empty GatewayNames = announces to all gateways
sharedService.Start()
```

### Connection Lifecycle

Use Velaros hooks for connection setup and teardown:

```go
router.UseOpen(func(ctx *velaros.Context) {
    // Runs once when client connects
    ctx.SetOnSocket("connectedAt", time.Now())
    log.Printf("Client connected: %s", ctx.SocketID())
    ctx.Next()
})

router.UseClose(func(ctx *velaros.Context) {
    // Runs once when client disconnects
    duration := time.Since(ctx.MustGetFromSocket("connectedAt").(time.Time))
    log.Printf("Client %s disconnected after %v", ctx.SocketID(), duration)
})
```

### Bidirectional Communication

Services can request data from clients:

```go
router.PublicBind("/monitor/start", func(ctx *velaros.Context) {
    // Start monitoring loop
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // Request metrics from client
            var metrics ClientMetrics
            err := ctx.RequestInto(MetricsRequest{
                Timestamp: time.Now().Unix(),
            }, &metrics)

            if err != nil {
                return  // Client disconnected
            }

            log.Printf("Client CPU: %.2f%%, Memory: %.2f%%",
                metrics.CPU, metrics.Memory)

        case <-ctx.Done():
            return  // Connection closed
        }
    }
})
```

Client handles server requests:

```javascript
ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);

    // Server requesting metrics
    if (msg.path === '/monitor/start') {
        ws.send(JSON.stringify({
            id: msg.id,  // Echo the ID for correlation
            data: {
                cpu: getCPUUsage(),
                memory: getMemoryUsage()
            }
        }));
    }
};
```

## Testing

Testing with Eurus is straightforward - use the local transport to run everything in-process:

```go
func TestServiceRouting(t *testing.T) {
    // Everything runs in-process for testing
    transport := localtransport.New()

    // Set up gateway
    gateway := eurus.NewGateway("test-gateway", transport)
    gateway.Start()
    defer gateway.Stop()

    // Set up service
    router := velaros.NewRouter()
    router.Use(json.Middleware())
    router.PublicBind("/test", func(ctx *velaros.Context) {
        ctx.Reply(map[string]string{"result": "success"})
    })

    service := eurus.NewService("test-service", transport, router)
    service.Start()
    defer service.Stop()

    // Verify the gateway can route to our service
    assert.True(t, gateway.CanServePath("/test"))
}

func TestSocketPinning(t *testing.T) {
    transport := localtransport.New()

    // Start gateway and multiple service instances
    gateway := eurus.NewGateway("gateway", transport)
    gateway.Start()

    var instances [3]*eurus.Service
    for i := 0; i < 3; i++ {
        router := velaros.NewRouter()
        router.Use(json.Middleware())
        router.PublicBind("/test", handler)

        instances[i] = eurus.NewService("service", transport, router)
        instances[i].Start()
    }

    // Test that messages from same connection go to same instance
    // (Implementation depends on your test framework)
}
```

Run with race detector:

```bash
go test -race
```

## Comparison with Zephyr

Eurus and [Zephyr](https://github.com/telemetrytv/Zephyr) are sister projects - same architecture, different protocols:

| | Zephyr | Eurus |
|-|--------|-------|
| **Protocol** | HTTP | WebSocket |
| **Router** | Navaros | Velaros |
| **State** | Stateless | Stateful |
| **Load Balance** | Per request | Per connection |
| **Best For** | REST APIs | Real-time |

Use both together for complete coverage:

```go
// HTTP API with Zephyr
httpGateway := zephyr.NewGateway("http-gateway", transport)
http.Handle("/api/", httpGateway)

// WebSocket with Eurus
wsGateway := eurus.NewGateway("ws-gateway", transport)
wsRouter := velaros.NewRouter()
wsRouter.Use(json.Middleware())
wsRouter.Use(wsGateway)
http.Handle("/ws", wsRouter)

http.ListenAndServe(":8080", nil)
```

## Production Considerations

### Observability

Add metrics for monitoring:

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    activeConnections = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "eurus_active_connections",
            Help: "Active WebSocket connections",
        },
        []string{"gateway"},
    )

    messagesRouted = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "eurus_messages_total",
            Help: "Total messages routed",
        },
        []string{"gateway", "service"},
    )
)
```

### Health Monitoring

Implement health endpoints on services (using `Bind()` for internal access):

```go
router.Bind("/health", func(ctx *velaros.Context) {
    health := CheckHealth()
    if !health.OK {
        ctx.Status = 503
    }
    ctx.Reply(health)
})
```

## Help Welcome

If you want to support this project by throwing me some coffee money, it's greatly appreciated.

[![sponsor](https://img.shields.io/static/v1?label=Sponsor&message=%E2%9D%A4&logo=GitHub&color=%23fe8e86)](https://github.com/sponsors/RobertWHurst)

If you're interested in providing feedback or would like to contribute, please feel free to do so. I recommend first opening an issue expressing your feedback or intent to contribute a change, from there we can consider your feedback or guide your contribution efforts. Any and all help is greatly appreciated since this is an open source effort after all.

Thank you!

## License

MIT License - see [LICENSE](LICENSE) for details.

## Related Projects

- [Velaros](https://github.com/RobertWHurst/Velaros) - WebSocket framework for Go (Eurus services use Velaros routers)
- [Zephyr](https://github.com/telemetrytv/Zephyr) - HTTP microservices gateway (sister project)
- [Navaros](https://github.com/RobertWHurst/Navaros) - HTTP framework for Go (Zephyr services use Navaros routers)
- [Conduit](https://github.com/RobertWHurst/Conduit) - Transport-agnostic messaging framework