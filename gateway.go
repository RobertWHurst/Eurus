package eurus

import (
	"context"
	"fmt"
	"net/http"

	"github.com/RobertWHurst/navaros"
	"github.com/RobertWHurst/velaros"
	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type MessageWithPath struct {
	Path string `json:"path"`
}

type Gateway struct {
	Name           string
	Transport      Transport
	gsi            *GatewayServiceIndexer
	MessageDecoder velaros.MessageDecoder
}

func NewGateway(name string, transport Transport) *Gateway {
	return &Gateway{
		Name:           name,
		Transport:      transport,
		MessageDecoder: velaros.DefaultMessageDecoder,
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

	return g.Transport.AnnounceGateway(&GatewayDescriptor{
		Name:               g.Name,
		ServiceDescriptors: g.gsi.ServiceDescriptors,
	})
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

	socketID := uuid.NewString()

	if err := g.Transport.BindOutboundSocketMessage(socketID, func(messageData []byte) error {
		return conn.Write(context.TODO(), websocket.MessageText, messageData)
	}); err != nil {
		conn.Close(websocket.StatusInternalError, "failed to bind outbound socket message")
		panic(err)
	}

	if err := g.Transport.BindSocketClose(socketID, func(reason int) error {
		return conn.Close(websocket.StatusCode(reason), "")
	}); err != nil {
		conn.Close(websocket.StatusInternalError, "failed to bind socket close")
		panic(err)
	}

	for {
		messageKind, messageData, err := conn.Read(context.TODO())
		if err != nil {
			reason := websocket.CloseStatus(err)
			if reason == -1 {
				// fixme: handle read error
				reason = websocket.StatusInternalError
			}

			g.Transport.SocketClose(socketID, int(reason))
			break
		}

		if messageKind != websocket.MessageText {
			// fixme: Handle invalid message type
			continue
		}

		message, err := g.MessageDecoder(messageData)
		if err != nil {
			// fixme: Handle 400 - missing path
			continue
		}

		serviceName, ok := g.gsi.ResolveService(message.Path)
		if !ok {
			// fixme: Handle 404
			continue
		}

		if err := g.Transport.InboundSocketMessage(serviceName, socketID, messageData); err != nil {
			// fixme: Handle error
			fmt.Println("Error handling inbound socket message:", err)
		}
	}
}
