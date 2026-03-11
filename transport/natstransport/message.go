package natstransport

import (
	"time"

	"github.com/telemetryos/go-debug/debug"
)

var (
	transportNatsMessageDebug = debug.Bind("eurus:transport:nats:message")
)

const MaxChunkSize = 1024 * 16
const MessageTimeout = 30 * time.Second

type MessageAck struct {
	ChunkSubject string `msgpack:"chunkSubject"`
}

type MessageChunk struct {
	Index int    `msgpack:"index"`
	Data  []byte `msgpack:"data"`
	IsEOF bool   `msgpack:"eof"`
}

type MessageChunkAck struct {
	Index int  `msgpack:"index"`
	IsEOF bool `msgpack:"eof"`
}
