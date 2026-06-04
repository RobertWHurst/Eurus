package natstransport

import "testing"

func BenchmarkNamespace(b *testing.B) {
	// Simulates the hot path: building a subject from a 32-char generated ID,
	// which happens on every message sent/received.
	id := "aB3dEfGhIjKlMnOpQrStUvWxYz012345"
	b.ReportAllocs()
	for b.Loop() {
		_ = namespace("service", id, "message")
	}
}
