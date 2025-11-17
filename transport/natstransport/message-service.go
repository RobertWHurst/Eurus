package natstransport

import (
	"net/http"

	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

type ServiceMessageHeader struct {
	GatewayID               string                `msgpack:"gatewayID"`
	SocketID                string                `msgpack:"socketID"`
	Headers                 map[string][]string   `msgpack:"headers"`
	RemoteAddr              string                `msgpack:"remoteAddr"`
	MessageType             websocket.MessageType `msgpack:"messageType"`
	SocketAssociatedValues  map[string]any        `msgpack:"socketAssociatedValues"`
	MessageAssociatedValues map[string]any        `msgpack:"messageAssociatedValues"`
}

// MessageService sends a message from gateway to service
func (t *NatsTransport) MessageService(serviceID, gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage) error {
	transportNatsMessageDebug.Tracef("Sending message to service %s from gateway %s socket %s", serviceID, gatewayID, socketID)

	subject := namespace("service", serviceID, "message")

	header := &ServiceMessageHeader{
		GatewayID:               gatewayID,
		SocketID:                socketID,
		Headers:                 map[string][]string(connInfo.Headers),
		RemoteAddr:              connInfo.RemoteAddr,
		MessageType:             msg.Type,
		SocketAssociatedValues:  msg.SocketAssociatedValues,
		MessageAssociatedValues: msg.MessageAssociatedValues,
	}

	headerBytes, err := msgpack.Marshal(header)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal header: %v", err)
		return err
	}

	transportNatsMessageDebug.Trace("Sending message header, waiting for ack")
	ackMsg, err := t.NatsConnection.Request(subject, headerBytes, MessageTimeout)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to get ack: %v", err)
		return err
	}

	ack := &MessageAck{}
	if err := msgpack.Unmarshal(ackMsg.Data, ack); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal ack: %v", err)
		return err
	}

	chunkSubject := ack.ChunkSubject
	transportNatsMessageDebug.Tracef("Received chunk subject: %s", chunkSubject)

	transportNatsMessageDebug.Tracef("Streaming %d bytes", len(msg.Data))
	offset := 0
	index := 0
	for offset < len(msg.Data) {
		end := offset + MaxChunkSize
		if end > len(msg.Data) {
			end = len(msg.Data)
		}

		chunk := &MessageChunk{
			Index: index,
			Data:  msg.Data[offset:end],
			IsEOF: false,
		}

		chunkBytes, err := msgpack.Marshal(chunk)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to marshal chunk: %v", err)
			return err
		}

		transportNatsMessageDebug.Tracef("Sending chunk %d (%d bytes)", index, len(chunk.Data))
		messageChunkAckMsg, err := t.NatsConnection.Request(chunkSubject, chunkBytes, MessageTimeout)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to publish chunk: %v", err)
			return err
		}

		messageChunkAck := &MessageChunkAck{}
		if err := msgpack.Unmarshal(messageChunkAckMsg.Data, messageChunkAck); err != nil {
			transportNatsMessageDebug.Tracef("Failed to unmarshal chunk ack: %v", err)
			return err
		}

		transportNatsMessageDebug.Tracef("Received chunk ack for index %d (EOF: %v)", messageChunkAck.Index, messageChunkAck.IsEOF)

		offset = end
		index++
	}

	eofChunk := &MessageChunk{
		Index: index,
		IsEOF: true,
	}
	eofBytes, err := msgpack.Marshal(eofChunk)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal EOF chunk: %v", err)
		return err
	}

	transportNatsMessageDebug.Trace("Sending EOF chunk")
	messageChunkAckMsg, err := t.NatsConnection.Request(chunkSubject, eofBytes, MessageTimeout)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to publish EOF chunk: %v", err)
		return err
	}

	messageChunkAck := &MessageChunkAck{}
	if err := msgpack.Unmarshal(messageChunkAckMsg.Data, messageChunkAck); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal EOF chunk ack: %v", err)
		return err
	}

	transportNatsMessageDebug.Trace("Message sent successfully")
	return nil
}

// BindMessageService binds handler for messages coming to a service
func (t *NatsTransport) BindMessageService(serviceID string, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) error {
	transportNatsMessageDebug.Tracef("Binding message handler for service %s", serviceID)

	subject := namespace("service", serviceID, "message")

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		if err := t.handleServiceMessage(msg, handler); err != nil {
			transportNatsMessageDebug.Tracef("Error handling service message: %v", err)
		}
	})

	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to subscribe: %v", err)
		return err
	}

	t.unbindMessageService[serviceID] = func() error {
		transportNatsMessageDebug.Tracef("Unbinding message handler for service %s", serviceID)
		return sub.Unsubscribe()
	}

	transportNatsMessageDebug.Trace("Message handler bound successfully")
	return nil
}

func (t *NatsTransport) handleServiceMessage(msg *nats.Msg, handler func(gatewayID, socketID string, connInfo *velaros.ConnectionInfo, msg *velaros.SocketMessage)) error {
	header := &ServiceMessageHeader{}
	if err := msgpack.Unmarshal(msg.Data, header); err != nil {
		transportNatsMessageDebug.Tracef("Failed to unmarshal header: %v", err)
		return err
	}

	transportNatsMessageDebug.Tracef("Received message header for socket %s", header.SocketID)

	chunkSubject := nats.NewInbox()
	chunkSub, err := t.NatsConnection.SubscribeSync(chunkSubject)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to subscribe to chunks: %v", err)
		return err
	}
	defer chunkSub.Unsubscribe()

	ack := &MessageAck{
		ChunkSubject: chunkSubject,
	}
	ackBytes, err := msgpack.Marshal(ack)
	if err != nil {
		transportNatsMessageDebug.Tracef("Failed to marshal ack: %v", err)
		return err
	}

	transportNatsMessageDebug.Tracef("Sending ack with chunk subject: %s", chunkSubject)
	if err := t.NatsConnection.Publish(msg.Reply, ackBytes); err != nil {
		transportNatsMessageDebug.Tracef("Failed to send ack: %v", err)
		return err
	}

	var assembled []byte
	for {
		chunkMsg, err := chunkSub.NextMsg(MessageTimeout)
		if err != nil {
			transportNatsMessageDebug.Tracef("Error receiving chunk: %v", err)
			return err
		}
		chunk := &MessageChunk{}
		if err := msgpack.Unmarshal(chunkMsg.Data, chunk); err != nil {
			transportNatsMessageDebug.Tracef("Failed to unmarshal chunk: %v", err)
			return err
		}

		chunkAck := &MessageChunkAck{
			Index: chunk.Index,
			IsEOF: chunk.IsEOF,
		}
		chunkAckBytes, err := msgpack.Marshal(chunkAck)
		if err != nil {
			transportNatsMessageDebug.Tracef("Failed to marshal chunk ack: %v", err)
			return err
		}
		if err := t.NatsConnection.Publish(chunkMsg.Reply, chunkAckBytes); err != nil {
			transportNatsMessageDebug.Tracef("Failed to send chunk ack: %v", err)
			return err
		}

		transportNatsMessageDebug.Tracef("Received chunk %d (%d bytes, EOF: %v)", chunk.Index, len(chunk.Data), chunk.IsEOF)

		assembled = append(assembled, chunk.Data...)

		if chunk.IsEOF {
			transportNatsMessageDebug.Tracef("Received EOF, delivering assembled message (%d bytes)", len(assembled))
			break
		}
	}

	connInfo := &velaros.ConnectionInfo{
		Headers:    http.Header(header.Headers),
		RemoteAddr: header.RemoteAddr,
	}
	handler(header.GatewayID, header.SocketID, connInfo, &velaros.SocketMessage{
		Type:                    header.MessageType,
		Data:                    assembled,
		SocketAssociatedValues:  header.SocketAssociatedValues,
		MessageAssociatedValues: header.MessageAssociatedValues,
	})

	transportNatsMessageDebug.Trace("Message delivered successfully")
	return nil
}

// UnbindMessageService unbinds the message handler for a service
func (t *NatsTransport) UnbindMessageService(serviceID string) error {
	if unbind, ok := t.unbindMessageService[serviceID]; ok {
		return unbind()
	}
	return nil
}
