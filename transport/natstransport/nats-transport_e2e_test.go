package natstransport

import (
	"bytes"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RobertWHurst/eurus"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// newTestServer starts an in-process NATS server on a random port and returns
// its client URL. The server is shut down when the test ends.
func newTestServer(t *testing.T) string {
	t.Helper()
	s, err := natsserver.NewServer(&natsserver.Options{Host: "127.0.0.1", Port: -1})
	require.NoError(t, err)
	go s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second), "server failed to start")
	t.Cleanup(s.Shutdown)
	return s.ClientURL()
}

// newTestConn opens a NATS connection to url and closes it when the test ends.
func newTestConn(t *testing.T, url string) *nats.Conn {
	t.Helper()
	conn, err := nats.Connect(url)
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn
}

// --- Message round trips (P1: inline fast path + streaming fallback) ---

func TestE2E_MessageService_RoundTrip(t *testing.T) {
	sizes := map[string]int{
		"empty":          0,
		"inline_small":   100,
		"boundary_16k":   MaxChunkSize, // largest single-frame inline payload
		"streaming_40k":  40 * 1024,    // forces the chunk-streaming path
		"streaming_200k": 200 * 1024,   // many chunks
	}
	for name, size := range sizes {
		t.Run(name, func(t *testing.T) {
			conn := newTestConn(t, newTestServer(t))
			recv := New(conn)
			send := New(conn)

			type received struct {
				gatewayID string
				socketID  string
				connInfo  *velaros.ConnectionInfo
				msg       *velaros.SocketMessage
			}
			gotCh := make(chan received, 1)
			require.NoError(t, recv.BindMessageService("svc-1", func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
				gotCh <- received{gatewayID, socketID, connInfo, msg}
			}))
			t.Cleanup(func() { _ = recv.UnbindMessageService("svc-1") })

			data := bytes.Repeat([]byte("a"), size)
			err := send.MessageService("svc-1", "gw-1", "sock-1",
				&velaros.ConnectionInfo{RemoteAddr: "1.2.3.4"},
				&velaros.SocketMessage{Type: websocket.MessageText, RawData: data, Data: data})
			require.NoError(t, err)

			select {
			case got := <-gotCh:
				require.Equal(t, "gw-1", got.gatewayID)
				require.Equal(t, "sock-1", got.socketID)
				require.Equal(t, "1.2.3.4", got.connInfo.RemoteAddr)
				require.Equal(t, websocket.MessageText, got.msg.Type)
				require.True(t, bytes.Equal(data, got.msg.RawData), "RawData mismatch")
				require.True(t, bytes.Equal(data, got.msg.Data), "Data mismatch")
			case <-time.After(5 * time.Second):
				t.Fatalf("timeout waiting for service message (size=%d)", size)
			}
		})
	}
}

func TestE2E_MessageGateway_RoundTrip(t *testing.T) {
	sizes := map[string]int{
		"inline_small":  100,
		"boundary_16k":  MaxChunkSize,
		"streaming_40k": 40 * 1024,
	}
	for name, size := range sizes {
		t.Run(name, func(t *testing.T) {
			conn := newTestConn(t, newTestServer(t))
			recv := New(conn)
			send := New(conn)

			type received struct {
				socketID string
				msg      *velaros.SocketMessage
			}
			gotCh := make(chan received, 1)
			require.NoError(t, recv.BindMessageGateway("gw-9", func(socketID string, msg *velaros.SocketMessage) {
				gotCh <- received{socketID, msg}
			}))
			t.Cleanup(func() { _ = recv.UnbindMessageGateway("gw-9") })

			data := bytes.Repeat([]byte("b"), size)
			err := send.MessageGateway("gw-9", "sock-2",
				&velaros.SocketMessage{Type: websocket.MessageBinary, RawData: data, Data: data})
			require.NoError(t, err)

			select {
			case got := <-gotCh:
				require.Equal(t, "sock-2", got.socketID)
				require.True(t, bytes.Equal(data, got.msg.RawData), "RawData mismatch")
			case <-time.After(5 * time.Second):
				t.Fatalf("timeout waiting for gateway message (size=%d)", size)
			}
		})
	}
}

// MessageService should error when no service handler is bound (request times
// out), rather than hang forever or panic.
func TestE2E_MessageService_NoHandler(t *testing.T) {
	conn := newTestConn(t, newTestServer(t))
	send := New(conn)

	// Use a short timeout via a payload that takes the inline path; the request
	// has no responder so it returns nats.ErrNoResponders quickly.
	err := send.MessageService("absent-svc", "gw", "sock",
		&velaros.ConnectionInfo{}, &velaros.SocketMessage{Type: websocket.MessageText, Data: []byte("hi")})
	require.Error(t, err)
}

// --- Announce ---

func TestE2E_AnnounceService(t *testing.T) {
	conn := newTestConn(t, newTestServer(t))
	recv := New(conn)
	send := New(conn)

	gotCh := make(chan *eurus.ServiceDescriptor, 1)
	require.NoError(t, recv.BindServiceAnnounce(func(d *eurus.ServiceDescriptor) {
		gotCh <- d
	}))
	t.Cleanup(func() { _ = recv.UnbindServiceAnnounce() })

	require.NoError(t, send.AnnounceService(&eurus.ServiceDescriptor{Name: "svc", ID: "id-1"}))

	select {
	case d := <-gotCh:
		require.Equal(t, "svc", d.Name)
		require.Equal(t, "id-1", d.ID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for service announce")
	}
}

func TestE2E_AnnounceGateway(t *testing.T) {
	conn := newTestConn(t, newTestServer(t))
	recv := New(conn)
	send := New(conn)

	gotCh := make(chan *eurus.GatewayDescriptor, 1)
	require.NoError(t, recv.BindGatewayAnnounce(func(d *eurus.GatewayDescriptor) {
		gotCh <- d
	}))
	t.Cleanup(func() { _ = recv.UnbindGatewayAnnounce() })

	require.NoError(t, send.AnnounceGateway(&eurus.GatewayDescriptor{Name: "gw"}))

	select {
	case d := <-gotCh:
		require.Equal(t, "gw", d.Name)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for gateway announce")
	}
}

// R1: a malformed announcement from any peer must not crash the subscriber.
// After garbage is published, a subsequent valid announcement must still arrive.
func TestE2E_MalformedAnnounceIsDropped(t *testing.T) {
	conn := newTestConn(t, newTestServer(t))
	recv := New(conn)
	send := New(conn)

	gotCh := make(chan *eurus.ServiceDescriptor, 1)
	require.NoError(t, recv.BindServiceAnnounce(func(d *eurus.ServiceDescriptor) {
		gotCh <- d
	}))
	t.Cleanup(func() { _ = recv.UnbindServiceAnnounce() })

	// Publish garbage directly to the announce subject — previously this panicked
	// the subscription callback.
	require.NoError(t, conn.Publish(namespace("service.announce"), []byte("not-msgpack-\xff\x00")))
	require.NoError(t, conn.Flush())

	// A valid announcement afterward must still be delivered, proving the
	// subscription survived the bad message.
	require.NoError(t, send.AnnounceService(&eurus.ServiceDescriptor{Name: "after-garbage", ID: "ok"}))

	select {
	case d := <-gotCh:
		require.Equal(t, "after-garbage", d.Name)
	case <-time.After(5 * time.Second):
		t.Fatal("subscription did not survive a malformed announcement")
	}
}

// --- Socket closed ---

func TestE2E_SocketClosed(t *testing.T) {
	conn := newTestConn(t, newTestServer(t))
	recv := New(conn)
	send := New(conn)

	type closed struct {
		socketID string
		status   websocket.StatusCode
		reason   string
	}
	gotCh := make(chan closed, 1)
	require.NoError(t, recv.BindSocketClosed(func(socketID string, status websocket.StatusCode, reason string) {
		gotCh <- closed{socketID, status, reason}
	}))
	t.Cleanup(func() { _ = recv.UnbindSocketClosed() })

	require.NoError(t, send.ClosedSocket("sock-x", websocket.StatusGoingAway, "bye"))

	select {
	case c := <-gotCh:
		require.Equal(t, "sock-x", c.socketID)
		require.Equal(t, websocket.StatusGoingAway, c.status)
		require.Equal(t, "bye", c.reason)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for socket closed event")
	}
}

// --- Heartbeat ---

func TestE2E_Heartbeat(t *testing.T) {
	conn := newTestConn(t, newTestServer(t))
	gw := New(conn)
	svc := New(conn)

	require.NoError(t, gw.BindSocketHeartbeat("gw-h", func(serviceID, socketID string) bool {
		return socketID == "alive"
	}))
	t.Cleanup(func() { _ = gw.UnbindSocketHeartbeat("gw-h") })

	// Alive socket -> nil error.
	require.NoError(t, svc.HeartbeatSocket("gw-h", "svc", "alive"))
	// Unknown socket -> error.
	require.Error(t, svc.HeartbeatSocket("gw-h", "svc", "dead"))
}

// --- R2: concurrent bind/unbind on a shared transport must be race-free ---
// Run with `-race` to exercise the mutex guarding the unbind maps.
func TestE2E_ConcurrentBindUnbind(t *testing.T) {
	conn := newTestConn(t, newTestServer(t))
	tr := New(conn)

	var wg sync.WaitGroup
	var errs atomic.Int32
	const n = 16
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a'+i)) + "-svc"
			if err := tr.BindMessageService(id, func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
			}); err != nil {
				errs.Add(1)
				return
			}
			if err := tr.UnbindMessageService(id); err != nil {
				errs.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.Zero(t, errs.Load())
}

// Sanity: confirm a small payload actually marshals under the inline threshold,
// guarding the assumption behind the fast path.
func TestE2E_InlineThresholdAssumption(t *testing.T) {
	b, err := msgpack.Marshal(&ServiceMessagePayload{Data: bytes.Repeat([]byte("x"), 1024)})
	require.NoError(t, err)
	require.LessOrEqual(t, len(b), MaxChunkSize)
}
