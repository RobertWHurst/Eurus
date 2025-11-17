package eurus

import (
	"context"
	"net/http"

	"github.com/RobertWHurst/velaros"
)

type ConnectionMessage struct {
	MessageType velaros.MessageType
	Data        []byte
}

type Connection struct {
	transport              Transport
	gatewayID              string
	socketID               string
	headers                http.Header
	remoteAttr             string
	messageChan            chan *velaros.SocketMessage
	serializableKeys       []string
	serializableSocketKeys []string

	closeStatus velaros.Status
	closeReason string

	onClosed  func()
	ctxCancel context.CancelFunc
	ctx       context.Context
}

var _ velaros.SocketConnection = &Connection{}

func NewConnection(transport Transport, gatewayID, socketID string, info *velaros.ConnectionInfo, onClosed func(), serializableKeys, serializableSocketKeys []string) *Connection {
	ctx, cancel := context.WithCancel(context.Background())

	return &Connection{
		transport:              transport,
		gatewayID:              gatewayID,
		socketID:               socketID,
		headers:                info.Headers,
		remoteAttr:             info.RemoteAddr,
		messageChan:            make(chan *velaros.SocketMessage, 100),
		serializableKeys:       serializableKeys,
		serializableSocketKeys: serializableSocketKeys,

		onClosed:  onClosed,
		ctxCancel: cancel,
		ctx:       ctx,
	}
}

func (c *Connection) HandleMessage(msg *velaros.SocketMessage) {
	c.messageChan <- msg
}

func (c *Connection) HandleClose(status velaros.Status, reason string) {
	c.onClosed()
	c.ctxCancel()
	c.closeStatus = status
	c.closeReason = reason
}

func (c *Connection) Read(socketCtx context.Context) (*velaros.SocketMessage, error) {
	select {
	// Cancelled by service shutdown or connection close
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	// Cancelled by service handler
	case <-socketCtx.Done():
		return nil, socketCtx.Err()
	// New message received
	case msg := <-c.messageChan:
		return msg, nil
	}
}

func (c *Connection) Write(ctx context.Context, msg *velaros.SocketMessage) error {
	filteredMsg := &velaros.SocketMessage{
		Type:                    msg.Type,
		Data:                    msg.Data,
		MessageAssociatedValues: c.filterAssociatedValues(msg.MessageAssociatedValues, c.serializableKeys),
		SocketAssociatedValues:  c.filterAssociatedValues(msg.SocketAssociatedValues, c.serializableSocketKeys),
	}

	err := c.transport.MessageGateway(c.gatewayID, c.socketID, filteredMsg)
	if err != nil {
		c.HandleClose(velaros.StatusInternalError, "Failed to write message to gateway")
	}
	return err
}

func (c *Connection) filterAssociatedValues(values map[string]any, whitelist []string) map[string]any {
	if len(whitelist) == 0 {
		return nil
	}
	filtered := make(map[string]any, len(whitelist))
	for _, key := range whitelist {
		if val, ok := values[key]; ok {
			filtered[key] = val
		}
	}
	return filtered
}

func (c *Connection) Close(status velaros.Status, reason string) error {
	return c.transport.ClosedSocket(c.socketID, status, reason)
}
