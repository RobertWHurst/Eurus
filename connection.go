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
	transport   Transport
	gatewayID   string
	socketID    string
	headers     http.Header
	messageChan chan *ConnectionMessage

	closeStatus velaros.Status
	closeReason string

	onClosed  func()
	ctxCancel context.CancelFunc
	ctx       context.Context
}

var _ velaros.SocketConnection = &Connection{}

func NewConnection(transport Transport, gatewayID, socketID string, headers http.Header, onClosed func()) *Connection {
	ctx, cancel := context.WithCancel(context.Background())

	return &Connection{
		transport:   transport,
		gatewayID:   gatewayID,
		socketID:    socketID,
		headers:     headers,
		messageChan: make(chan *ConnectionMessage, 100),

		onClosed:  onClosed,
		ctxCancel: cancel,
		ctx:       ctx,
	}
}

func (c *Connection) HandleRawMessage(messageType velaros.MessageType, data []byte) {
	c.messageChan <- &ConnectionMessage{
		MessageType: messageType,
		Data:        data,
	}
}

func (c *Connection) HandleClose(status velaros.Status, reason string) {
	c.onClosed()
	c.ctxCancel()
	c.closeStatus = status
	c.closeReason = reason
}

func (c *Connection) Read(socketCtx context.Context) (velaros.MessageType, []byte, error) {
	select {
	// Cancelled by service shutdown or connection close
	case <-c.ctx.Done():
		return 0, nil, c.ctx.Err()
	// Cancelled by service handler
	case <-socketCtx.Done():
		return 0, nil, socketCtx.Err()
	// New message received
	case msg := <-c.messageChan:
		return msg.MessageType, msg.Data, nil
	}
}

func (c *Connection) Write(ctx context.Context, messageType velaros.MessageType, data []byte) error {
	err := c.transport.MessageGateway(c.gatewayID, c.socketID, messageType, data)
	if err != nil {
		c.HandleClose(velaros.StatusInternalError, "Failed to write message to gateway")
	}
	return err
}

func (c *Connection) Close(status velaros.Status, reason string) error {
	return c.transport.ClosedSocket(c.socketID, status, reason)
}
