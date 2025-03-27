package eurus

import (
	"fmt"
	"net/http"

	"github.com/RobertWHurst/navaros"
	"github.com/coder/websocket"
)

type Gateway struct {
	Name           string
	Transport      Transport
	gsi            *GatewayServiceIndexer
	MessageDecoder func([]byte) (*InboundMessage, error)
}

func NewGateway(name string, transport Transport) *Gateway {
	return &Gateway{
		Name:      name,
		Transport: transport,
	}
}

func (g *Gateway) Start() error {
	if g.gsi != nil {
		return fmt.Errorf("gateway already started")
	}

	g.gsi = &GatewayServiceIndexer{
		ServiceDescriptors: []*ServiceDescriptor{},
	}

	if g.Transport == nil {
		return fmt.Errorf(
			"cannot start local service. The associated gateway already handles " +
				"incoming requests",
		)
	}

	err := g.Transport.BindServiceAnnounce(func(serviceDescriptor *ServiceDescriptor) {
		announcingToThisGateway := len(serviceDescriptor.GatewayNames) == 0
		if !announcingToThisGateway {
			for _, gatewayName := range serviceDescriptor.GatewayNames {
				if gatewayName == g.Name {
					announcingToThisGateway = true
					break
				}
			}
		}
		if !announcingToThisGateway {
			return
		}

		if err := g.gsi.SetServiceDescriptor(serviceDescriptor); err != nil {
			panic(err)
		}
	})
	if err != nil {
		return err
	}
}

func (g *Gateway) Stop() {
	if g.gsi == nil {
		return
	}
	g.gsi = nil
	if err := g.Transport.UnbindServiceAnnounce(); err != nil {
		panic(err)
	}
}

func (g *Gateway) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	if g.isWebsocketUpgradeRequest(req) {
		g.handleWebsocketConnection(res, req)
		return
	}
	res.WriteHeader(400)
	if _, err := res.Write([]byte("Bad Request. Expected websocket upgrade request")); err != nil {
		panic(err)
	}
}

func (g *Gateway) Handle(ctx *navaros.Context) {
	if g.isWebsocketUpgradeRequest(ctx.Request()) {
		navaros.CtxInhibitResponse(ctx)
		g.handleWebsocketConnection(ctx.ResponseWriter(), ctx.Request())
		return
	}
	ctx.Next()
}

func (g *Gateway) isWebsocketUpgradeRequest(req *http.Request) bool {
	return req.Header.Get("Upgrade") == "websocket"
}

func (g *Gateway) handleWebsocketConnection(res http.ResponseWriter, req *http.Request) {
	conn, err := websocket.Accept(res, req, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		conn.Close(websocket.StatusInternalError, "failed to accept websocket connection")
		panic(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	socket := newSocket(conn, g.Transport, g.MessageDecoder)
	defer socket.close()

	for {
		if !socket.handleNextMessageWithNode(g.firstHandlerNode) {
			break
		}
	}
}
