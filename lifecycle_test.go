package eurus_test

import (
	"context"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus/v2"
	"github.com/RobertWHurst/eurus/v2/transport/localtransport"
	"github.com/RobertWHurst/velaros"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveStream subscribes a transport stream and serves it until the returned
// stop function is called. Subscriptions are live once this returns.
func serveStream(tb testing.TB, subscribeFn func(ctx context.Context) (eurus.Subscription, error)) (stop func()) {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	sub, err := subscribeFn(ctx)
	require.NoError(tb, err)
	done := make(chan error, 1)
	go func() { done <- sub.Serve(ctx) }()
	return func() {
		cancel()
		select {
		case err := <-done:
			require.NoError(tb, err)
		case <-time.After(5 * time.Second):
			tb.Fatal("subscription did not stop")
		}
	}
}

// startService runs service.Listen in the background and waits for the
// service to report ready. Cleanup stops the service and waits for Listen to
// return.
func startService(tb testing.TB, service *eurus.Service) error {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- service.Listen(ctx)
	}()
	select {
	case err := <-errCh:
		cancel()
		return err
	case <-service.Ready():
		tb.Cleanup(func() {
			cancel()
			<-errCh
		})
		return nil
	case <-time.After(5 * time.Second):
		cancel()
		tb.Fatal("service did not become ready")
		return nil
	}
}

// startGateway runs gateway.Connect in the background and waits for the
// gateway to report ready. Cleanup stops the gateway and waits for Connect to
// return.
func startGateway(tb testing.TB, gateway *eurus.Gateway) error {
	tb.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- gateway.Connect(ctx)
	}()
	select {
	case err := <-errCh:
		cancel()
		return err
	case <-gateway.Ready():
		tb.Cleanup(func() {
			cancel()
			<-errCh
		})
		return nil
	case <-time.After(5 * time.Second):
		cancel()
		tb.Fatal("gateway did not become ready")
		return nil
	}
}

func TestGateway_ConnectReportsReady(t *testing.T) {
	transport := localtransport.New()
	gateway := eurus.NewGateway("test-gateway", transport)

	require.NoError(t, startGateway(t, gateway))

	select {
	case <-gateway.Ready():
	default:
		t.Fatal("Ready channel should be closed while serving")
	}
}

func TestService_ListenReportsReady(t *testing.T) {
	transport := localtransport.New()
	service := eurus.NewService("test-service", transport, velaros.NewRouter())

	require.NoError(t, startService(t, service))

	select {
	case <-service.Ready():
	default:
		t.Fatal("Ready channel should be closed while serving")
	}
}

func TestGateway_SecondConcurrentConnectFails(t *testing.T) {
	transport := localtransport.New()
	gateway := eurus.NewGateway("test-gateway", transport)

	require.NoError(t, startGateway(t, gateway))

	err := gateway.Connect(context.Background())
	assert.ErrorIs(t, err, eurus.ErrGatewayAlreadyConnected)
}

func TestService_SecondConcurrentListenFails(t *testing.T) {
	transport := localtransport.New()
	service := eurus.NewService("test-service", transport, velaros.NewRouter())

	require.NoError(t, startService(t, service))

	err := service.Listen(context.Background())
	assert.ErrorIs(t, err, eurus.ErrServiceAlreadyListening)
}

func TestGateway_ReconnectAfterClose(t *testing.T) {
	transport := localtransport.New()
	gateway := eurus.NewGateway("test-gateway", transport)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- gateway.Connect(ctx) }()
	select {
	case <-gateway.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not become ready")
	}

	cancel()
	require.NoError(t, <-errCh)

	// A gateway must be reusable after a clean shutdown.
	require.NoError(t, startGateway(t, gateway))
}

func TestService_RelistenAfterClose(t *testing.T) {
	transport := localtransport.New()
	service := eurus.NewService("test-service", transport, velaros.NewRouter())

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- service.Listen(ctx) }()
	select {
	case <-service.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("service did not become ready")
	}

	cancel()
	require.NoError(t, <-errCh)

	require.NoError(t, startService(t, service))
}

func TestGateway_CloseBeforeConnectIsNoop(t *testing.T) {
	transport := localtransport.New()
	gateway := eurus.NewGateway("test-gateway", transport)
	require.NoError(t, gateway.Close())
}

func TestService_CloseBeforeListenIsNoop(t *testing.T) {
	transport := localtransport.New()
	service := eurus.NewService("test-service", transport, velaros.NewRouter())
	require.NoError(t, service.Close())
}

func TestGateway_CloseUnblocksConnect(t *testing.T) {
	transport := localtransport.New()
	gateway := eurus.NewGateway("test-gateway", transport)

	errCh := make(chan error, 1)
	go func() { errCh <- gateway.Connect(context.Background()) }()
	select {
	case <-gateway.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("gateway did not become ready")
	}

	require.NoError(t, gateway.Close())

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Connect did not return after Close")
	}
}

func TestGateway_ConnectWithoutTransportFails(t *testing.T) {
	gateway := eurus.NewGateway("test-gateway", nil)
	err := gateway.Connect(context.Background())
	assert.ErrorIs(t, err, eurus.ErrNoTransport)
}

func TestService_ListenWithoutTransportFails(t *testing.T) {
	service := eurus.NewService("test-service", nil, velaros.NewRouter())
	err := service.Listen(context.Background())
	assert.ErrorIs(t, err, eurus.ErrNoTransport)
}
