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
	"github.com/vmihailenco/msgpack/v5"
)

type routePolicy struct {
	Auth      string `msgpack:"auth" json:"auth"`
	RateLimit int    `msgpack:"rateLimit" json:"rateLimit"`
}

func TestRouteDescriptor_MetadataSurvivesMsgpackAsTypedValue(t *testing.T) {
	route, err := eurus.NewRouteDescriptor("/api/items/:id")
	require.NoError(t, err)
	route.Metadata = routePolicy{Auth: "required", RateLimit: 100}

	data, err := msgpack.Marshal(route)
	require.NoError(t, err)

	decoded := &eurus.RouteDescriptor{}
	require.NoError(t, msgpack.Unmarshal(data, decoded))

	var policy routePolicy
	require.NoError(t, decoded.UnmarshalMetadata(&policy))
	assert.Equal(t, "required", policy.Auth)
	assert.Equal(t, 100, policy.RateLimit)
}

func TestRouteDescriptor_UnmarshalMetadataOnLocalDescriptor(t *testing.T) {
	route, err := eurus.NewRouteDescriptor("/api/items/:id")
	require.NoError(t, err)
	route.Metadata = routePolicy{Auth: "required", RateLimit: 5}

	var policy routePolicy
	require.NoError(t, route.UnmarshalMetadata(&policy))
	assert.Equal(t, "required", policy.Auth)
	assert.Equal(t, 5, policy.RateLimit)
}

func TestRouteDescriptor_UnmarshalMetadataWithoutMetadata(t *testing.T) {
	route, err := eurus.NewRouteDescriptor("/api/items")
	require.NoError(t, err)

	var policy routePolicy
	err = route.UnmarshalMetadata(&policy)
	assert.ErrorIs(t, err, eurus.ErrNoRouteMetadata)
}

// TestGateway_RouteMetadataAvailableToEdgeMiddleware exercises the whole
// mechanism: velaros WithMetadata at the bind site → service announcement →
// gateway index → DescriptorMiddleware → typed decode at the edge, before
// the message is dispatched to the service.
func TestGateway_RouteMetadataAvailableToEdgeMiddleware(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	require.NoError(t, startGateway(t, gateway))

	serviceRouter := velaros.NewRouter()
	serviceRouter.Use(velarosjson.Middleware())
	serviceRouter.PublicBind("/secure/echo", velaros.WithMetadata(routePolicy{
		Auth:      "required",
		RateLimit: 42,
	}), func(ctx *velaros.Context) {
		ctx.Send(map[string]string{"status": "ok"})
	})

	service := eurus.NewService("secure-service", transport, serviceRouter)
	require.NoError(t, startService(t, service))

	policyCh := make(chan routePolicy, 1)

	edgeRouter := velaros.NewRouter()
	edgeRouter.Use(velarosjson.Middleware())
	edgeRouter.Use(gateway.DescriptorMiddleware())
	edgeRouter.Use(func(ctx *velaros.Context) {
		if rd := eurus.RouteDescriptorFromContext(ctx); rd != nil {
			var policy routePolicy
			if err := rd.UnmarshalMetadata(&policy); err == nil {
				select {
				case policyCh <- policy:
				default:
				}
			}
		}
		ctx.Next()
	})
	edgeRouter.Use(gateway)

	server := httptest.NewServer(edgeRouter)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	require.NoError(t, wsjson.Write(ctx, conn, map[string]any{
		"path": "/secure/echo",
		"id":   "m1",
		"data": map[string]string{},
	}))

	var resp map[string]any
	require.NoError(t, wsjson.Read(ctx, conn, &resp))

	select {
	case policy := <-policyCh:
		assert.Equal(t, "required", policy.Auth)
		assert.Equal(t, 42, policy.RateLimit)
	case <-time.After(5 * time.Second):
		t.Fatal("edge middleware never saw route metadata")
	}
}

func TestGateway_DescriptorMiddleware_SetsServiceDescriptors(t *testing.T) {
	transport := localtransport.New()

	gateway := eurus.NewGateway("test-gateway", transport)
	require.NoError(t, startGateway(t, gateway))

	serviceRouter := velaros.NewRouter()
	serviceRouter.Use(velarosjson.Middleware())
	serviceRouter.PublicBind("/api/thing", func(ctx *velaros.Context) {
		ctx.Send(map[string]string{"status": "ok"})
	})
	service := eurus.NewService("api-service", transport, serviceRouter)
	require.NoError(t, startService(t, service))

	type seen struct {
		serviceDescriptor  *eurus.ServiceDescriptor
		serviceDescriptors []*eurus.ServiceDescriptor
	}
	seenCh := make(chan seen, 1)

	edgeRouter := velaros.NewRouter()
	edgeRouter.Use(velarosjson.Middleware())
	edgeRouter.Use(gateway.DescriptorMiddleware())
	edgeRouter.Use(func(ctx *velaros.Context) {
		select {
		case seenCh <- seen{
			serviceDescriptor:  eurus.ServiceDescriptorFromContext(ctx),
			serviceDescriptors: eurus.ServiceDescriptorsFromContext(ctx),
		}:
		default:
		}
		ctx.Next()
	})
	edgeRouter.Use(gateway)

	server := httptest.NewServer(edgeRouter)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()

	require.NoError(t, wsjson.Write(ctx, conn, map[string]any{
		"path": "/api/thing",
		"id":   "m1",
		"data": map[string]string{},
	}))
	var resp map[string]any
	require.NoError(t, wsjson.Read(ctx, conn, &resp))

	select {
	case got := <-seenCh:
		require.NotNil(t, got.serviceDescriptor)
		assert.Equal(t, "api-service", got.serviceDescriptor.Name)
		require.Len(t, got.serviceDescriptors, 1)
	case <-time.After(5 * time.Second):
		t.Fatal("edge middleware never saw service descriptors")
	}
}
