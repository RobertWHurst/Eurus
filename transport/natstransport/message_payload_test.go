package natstransport

import (
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestGatewayMessagePayload_MarshalUnmarshal(t *testing.T) {
	payload := &GatewayMessagePayload{
		RawData: []byte("raw data content"),
		Data:    []byte("processed data content"),
		Meta: map[string]any{
			"userId":  "user-123",
			"traceId": "trace-456",
			"count":   int64(42),
		},
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	unmarshaled := &GatewayMessagePayload{}
	if err := msgpack.Unmarshal(data, unmarshaled); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if string(unmarshaled.RawData) != string(payload.RawData) {
		t.Errorf("expected RawData %q, got %q", payload.RawData, unmarshaled.RawData)
	}
	if string(unmarshaled.Data) != string(payload.Data) {
		t.Errorf("expected Data %q, got %q", payload.Data, unmarshaled.Data)
	}
	if unmarshaled.Meta["userId"] != "user-123" {
		t.Errorf("expected userId user-123, got %v", unmarshaled.Meta["userId"])
	}
	if unmarshaled.Meta["traceId"] != "trace-456" {
		t.Errorf("expected traceId trace-456, got %v", unmarshaled.Meta["traceId"])
	}
	if unmarshaled.Meta["count"] != int64(42) {
		t.Errorf("expected count 42, got %v", unmarshaled.Meta["count"])
	}
}

func TestGatewayMessagePayload_EmptyFields(t *testing.T) {
	payload := &GatewayMessagePayload{
		RawData: nil,
		Data:    nil,
		Meta:    nil,
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	unmarshaled := &GatewayMessagePayload{}
	if err := msgpack.Unmarshal(data, unmarshaled); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if unmarshaled.RawData != nil {
		t.Errorf("expected RawData to be nil, got %v", unmarshaled.RawData)
	}
	if unmarshaled.Data != nil {
		t.Errorf("expected Data to be nil, got %v", unmarshaled.Data)
	}
	if unmarshaled.Meta != nil {
		t.Errorf("expected Meta to be nil, got %v", unmarshaled.Meta)
	}
}

func TestGatewayMessagePayload_OnlyRawData(t *testing.T) {
	payload := &GatewayMessagePayload{
		RawData: []byte("only raw"),
		Data:    nil,
		Meta:    nil,
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	unmarshaled := &GatewayMessagePayload{}
	if err := msgpack.Unmarshal(data, unmarshaled); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if string(unmarshaled.RawData) != "only raw" {
		t.Errorf("expected RawData 'only raw', got %q", unmarshaled.RawData)
	}
	if unmarshaled.Data != nil {
		t.Errorf("expected Data to be nil, got %v", unmarshaled.Data)
	}
	if unmarshaled.Meta != nil {
		t.Errorf("expected Meta to be nil, got %v", unmarshaled.Meta)
	}
}

func TestServiceMessagePayload_MarshalUnmarshal(t *testing.T) {
	payload := &ServiceMessagePayload{
		RawData: []byte("service raw data"),
		Data:    []byte("service processed data"),
		Meta: map[string]any{
			"sessionId": "session-789",
			"role":      "admin",
			"active":    true,
		},
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	unmarshaled := &ServiceMessagePayload{}
	if err := msgpack.Unmarshal(data, unmarshaled); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if string(unmarshaled.RawData) != string(payload.RawData) {
		t.Errorf("expected RawData %q, got %q", payload.RawData, unmarshaled.RawData)
	}
	if string(unmarshaled.Data) != string(payload.Data) {
		t.Errorf("expected Data %q, got %q", payload.Data, unmarshaled.Data)
	}
	if unmarshaled.Meta["sessionId"] != "session-789" {
		t.Errorf("expected sessionId session-789, got %v", unmarshaled.Meta["sessionId"])
	}
	if unmarshaled.Meta["role"] != "admin" {
		t.Errorf("expected role admin, got %v", unmarshaled.Meta["role"])
	}
	if unmarshaled.Meta["active"] != true {
		t.Errorf("expected active true, got %v", unmarshaled.Meta["active"])
	}
}

func TestServiceMessagePayload_EmptyFields(t *testing.T) {
	payload := &ServiceMessagePayload{
		RawData: nil,
		Data:    nil,
		Meta:    nil,
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	unmarshaled := &ServiceMessagePayload{}
	if err := msgpack.Unmarshal(data, unmarshaled); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if unmarshaled.RawData != nil {
		t.Errorf("expected RawData to be nil, got %v", unmarshaled.RawData)
	}
	if unmarshaled.Data != nil {
		t.Errorf("expected Data to be nil, got %v", unmarshaled.Data)
	}
	if unmarshaled.Meta != nil {
		t.Errorf("expected Meta to be nil, got %v", unmarshaled.Meta)
	}
}

func TestServiceMessagePayload_MetaOnly(t *testing.T) {
	payload := &ServiceMessagePayload{
		RawData: nil,
		Data:    nil,
		Meta: map[string]any{
			"apiKey":    "key-abc123",
			"timestamp": int64(1234567890),
		},
	}

	data, err := msgpack.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	unmarshaled := &ServiceMessagePayload{}
	if err := msgpack.Unmarshal(data, unmarshaled); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if unmarshaled.RawData != nil {
		t.Errorf("expected RawData to be nil, got %v", unmarshaled.RawData)
	}
	if unmarshaled.Data != nil {
		t.Errorf("expected Data to be nil, got %v", unmarshaled.Data)
	}
	if unmarshaled.Meta["apiKey"] != "key-abc123" {
		t.Errorf("expected apiKey key-abc123, got %v", unmarshaled.Meta["apiKey"])
	}
	if unmarshaled.Meta["timestamp"] != int64(1234567890) {
		t.Errorf("expected timestamp 1234567890, got %v", unmarshaled.Meta["timestamp"])
	}
}
