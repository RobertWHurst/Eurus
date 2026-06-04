package natstransport

import (
	"testing"

	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

// The inline fast path carries a marshaled ServiceMessagePayload in
// ServiceMessageInit.Payload. Verify the envelope round-trips header + payload,
// and that the streaming form omits the payload.
func TestServiceMessageInit_InlineRoundTrip(t *testing.T) {
	payloadBytes, err := msgpack.Marshal(&ServiceMessagePayload{
		RawData: []byte("raw"),
		Data:    []byte("data"),
		Meta:    map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	init := &ServiceMessageInit{
		Header: &ServiceMessageHeader{
			GatewayID:   "gw-1",
			SocketID:    "sock-1",
			RemoteAddr:  "1.2.3.4",
			MessageType: websocket.MessageText,
			Headers:     map[string][]string{"X-Test": {"a"}},
		},
		Stream:  false,
		Payload: payloadBytes,
	}

	b, err := msgpack.Marshal(init)
	if err != nil {
		t.Fatalf("marshal init: %v", err)
	}

	got := &ServiceMessageInit{}
	if err := msgpack.Unmarshal(b, got); err != nil {
		t.Fatalf("unmarshal init: %v", err)
	}

	if got.Stream {
		t.Errorf("expected Stream=false")
	}
	if got.Header.SocketID != "sock-1" || got.Header.GatewayID != "gw-1" {
		t.Errorf("header not preserved: %+v", got.Header)
	}
	if string(got.Payload) != string(payloadBytes) {
		t.Errorf("payload not preserved")
	}

	// The inline payload must decode back into the original ServiceMessagePayload.
	p := &ServiceMessagePayload{}
	if err := msgpack.Unmarshal(got.Payload, p); err != nil {
		t.Fatalf("unmarshal inline payload: %v", err)
	}
	if string(p.RawData) != "raw" || string(p.Data) != "data" || p.Meta["k"] != "v" {
		t.Errorf("decoded payload mismatch: %+v", p)
	}
}

func TestServiceMessageInit_StreamingOmitsPayload(t *testing.T) {
	b, err := msgpack.Marshal(&ServiceMessageInit{
		Header: &ServiceMessageHeader{SocketID: "s"},
		Stream: true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := &ServiceMessageInit{}
	if err := msgpack.Unmarshal(b, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.Stream {
		t.Errorf("expected Stream=true")
	}
	if got.Payload != nil {
		t.Errorf("expected nil payload for streaming init, got %v", got.Payload)
	}
}

func TestGatewayMessageInit_InlineRoundTrip(t *testing.T) {
	payloadBytes, err := msgpack.Marshal(&GatewayMessagePayload{
		RawData: []byte("graw"),
		Data:    []byte("gdata"),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	b, err := msgpack.Marshal(&GatewayMessageInit{
		Header:  &GatewayMessageHeader{SocketID: "sock-9", MessageType: websocket.MessageBinary},
		Stream:  false,
		Payload: payloadBytes,
	})
	if err != nil {
		t.Fatalf("marshal init: %v", err)
	}

	got := &GatewayMessageInit{}
	if err := msgpack.Unmarshal(b, got); err != nil {
		t.Fatalf("unmarshal init: %v", err)
	}
	if got.Stream || got.Header.SocketID != "sock-9" || string(got.Payload) != string(payloadBytes) {
		t.Errorf("gateway inline init not preserved: %+v", got)
	}
}

// handleServiceMessage is the consumer of the inline payload bytes returned by
// parseServiceMessage. Verify it decodes them and invokes the handler with the
// reconstructed SocketMessage and connection info.
func TestHandleServiceMessage_DecodesInlinePayload(t *testing.T) {
	assembled, err := msgpack.Marshal(&ServiceMessagePayload{
		RawData: []byte("raw"),
		Data:    []byte("data"),
		Meta:    map[string]any{"trace": "abc"},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	header := &ServiceMessageHeader{
		GatewayID:   "gw",
		SocketID:    "sock",
		MessageType: websocket.MessageText,
		Headers:     map[string][]string{"H": {"1"}},
		RemoteAddr:  "9.9.9.9",
	}

	tr := &NatsTransport{}
	called := false
	tr.handleServiceMessage(assembled, header, func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) {
		called = true
		if gatewayID != "gw" || socketID != "sock" {
			t.Errorf("ids mismatch: %s %s", gatewayID, socketID)
		}
		if connInfo.RemoteAddr != "9.9.9.9" || connInfo.Headers.Get("H") != "1" {
			t.Errorf("connInfo mismatch: %+v", connInfo)
		}
		if string(msg.RawData) != "raw" || string(msg.Data) != "data" || msg.Meta["trace"] != "abc" {
			t.Errorf("msg mismatch: %+v", msg)
		}
		if msg.Type != websocket.MessageText {
			t.Errorf("message type mismatch: %v", msg.Type)
		}
	})
	if !called {
		t.Fatalf("handler was not called")
	}
}

// handleGatewayMessage is the consumer of the inline payload for the gateway
// direction. Same verification.
func TestHandleGatewayMessage_DecodesInlinePayload(t *testing.T) {
	assembled, err := msgpack.Marshal(&GatewayMessagePayload{
		RawData: []byte("graw"),
		Data:    []byte("gdata"),
		Meta:    map[string]any{"n": int64(5)},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	header := &GatewayMessageHeader{SocketID: "s2", MessageType: websocket.MessageBinary}

	tr := &NatsTransport{}
	called := false
	tr.handleGatewayMessage(assembled, header, func(socketID string, msg *velaros.SocketMessage) {
		called = true
		if socketID != "s2" {
			t.Errorf("socketID mismatch: %s", socketID)
		}
		if string(msg.RawData) != "graw" || string(msg.Data) != "gdata" || msg.Meta["n"] != int64(5) {
			t.Errorf("msg mismatch: %+v", msg)
		}
	})
	if !called {
		t.Fatalf("handler was not called")
	}
}
