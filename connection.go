package eurus

import (
	"context"
	"net/http"
	"sync"

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
	remoteAttr  string
	messageChan chan *velaros.SocketMessage

	closeOnce   sync.Once
	closeMu     sync.Mutex
	closeStatus velaros.Status
	closeReason string

	onClosed  func()
	ctxCancel context.CancelFunc
	ctx       context.Context
}

var _ velaros.SocketConnection = &Connection{}

func NewConnection(transport Transport, gatewayID, socketID string, info *velaros.ConnectionInfo, onClosed func()) *Connection {
	ctx, cancel := context.WithCancel(context.Background())

	return &Connection{
		transport:   transport,
		gatewayID:   gatewayID,
		socketID:    socketID,
		headers:     info.Headers,
		remoteAttr:  info.RemoteAddr,
		messageChan: make(chan *velaros.SocketMessage),

		onClosed:  onClosed,
		ctxCancel: cancel,
		ctx:       ctx,
	}
}

func (c *Connection) GatewayID() string {
	return c.gatewayID
}

func (c *Connection) HandleMessage(msg *velaros.SocketMessage) {
	select {
	case c.messageChan <- msg:
	case <-c.ctx.Done():
	}
}

func (c *Connection) HandleClose(status velaros.Status, reason string) {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closeStatus = status
		c.closeReason = reason
		c.closeMu.Unlock()

		c.onClosed()
		c.ctxCancel()
	})
}

func (c *Connection) Read(readCtx context.Context) (*velaros.SocketMessage, error) {
	select {
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	case <-readCtx.Done():
		return nil, readCtx.Err()
	case msg := <-c.messageChan:
		return msg, nil
	}
}

func (c *Connection) Write(ctx context.Context, msg *velaros.SocketMessage) error {
	err := c.transport.MessageGateway(c.gatewayID, c.socketID, msg)
	if err != nil {
		c.HandleClose(velaros.StatusInternalError, "Failed to write message to gateway")
	}
	return err
}

func (c *Connection) Close(status velaros.Status, reason string) error {
	return c.transport.ClosedSocket(c.socketID, status, reason)
}
