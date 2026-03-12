package natstransport

import (
	"github.com/nats-io/nats.go"
	"github.com/telemetryos/go-debug/debug"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsHeartbeatDebug = debug.Bind("eurus:transport:nats:heartbeat")
)

type HeartbeatPayload struct {
	ID string `msgpack:"id"`
}

// SendServiceHeartbeat publishes the service's heartbeat to each listed gateway.
func (t *NatsTransport) SendServiceHeartbeat(serviceID string, gatewayIDs []string) error {
	payload := &HeartbeatPayload{ID: serviceID}
	data, err := msgpack.Marshal(payload)
	if err != nil {
		transportNatsHeartbeatDebug.Tracef("Failed to marshal service heartbeat payload: %v", err)
		return err
	}

	for _, gatewayID := range gatewayIDs {
		subject := namespace("gateway", gatewayID, "heartbeat")
		transportNatsHeartbeatDebug.Tracef("Sending service heartbeat to gateway %s on %s", gatewayID, subject)
		if err := t.NatsConnection.Publish(subject, data); err != nil {
			transportNatsHeartbeatDebug.Tracef("Failed to publish service heartbeat to gateway %s: %v", gatewayID, err)
			return err
		}
	}

	return nil
}

// BindServiceHeartbeat subscribes to heartbeats sent by services to this gateway.
func (t *NatsTransport) BindServiceHeartbeat(gatewayID string, handler func(serviceID string)) error {
	transportNatsHeartbeatDebug.Trace("Binding service heartbeat handler")

	subject := namespace("gateway", gatewayID, "heartbeat")
	transportNatsHeartbeatDebug.Tracef("Subscribing to service heartbeats on %s", subject)

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		payload := &HeartbeatPayload{}
		if err := msgpack.Unmarshal(msg.Data, payload); err != nil {
			transportNatsHeartbeatDebug.Tracef("Failed to unmarshal service heartbeat: %v", err)
			return
		}
		transportNatsHeartbeatDebug.Tracef("Received service heartbeat from %s", payload.ID)
		handler(payload.ID)
	})
	if err != nil {
		transportNatsHeartbeatDebug.Tracef("Failed to subscribe to service heartbeats: %v", err)
		return err
	}

	t.unbindServiceHeartbeat = func() error {
		transportNatsHeartbeatDebug.Trace("Unsubscribing from service heartbeats")
		return sub.Unsubscribe()
	}

	transportNatsHeartbeatDebug.Trace("Service heartbeat handler bound successfully")
	return nil
}

// UnbindServiceHeartbeat unsubscribes from service heartbeats.
func (t *NatsTransport) UnbindServiceHeartbeat() error {
	if t.unbindServiceHeartbeat != nil {
		return t.unbindServiceHeartbeat()
	}
	return nil
}

// SendGatewayHeartbeat publishes the gateway's heartbeat to each listed service.
func (t *NatsTransport) SendGatewayHeartbeat(gatewayID string, serviceIDs []string) error {
	payload := &HeartbeatPayload{ID: gatewayID}
	data, err := msgpack.Marshal(payload)
	if err != nil {
		transportNatsHeartbeatDebug.Tracef("Failed to marshal gateway heartbeat payload: %v", err)
		return err
	}

	for _, serviceID := range serviceIDs {
		subject := namespace("service", serviceID, "heartbeat")
		transportNatsHeartbeatDebug.Tracef("Sending gateway heartbeat to service %s on %s", serviceID, subject)
		if err := t.NatsConnection.Publish(subject, data); err != nil {
			transportNatsHeartbeatDebug.Tracef("Failed to publish gateway heartbeat to service %s: %v", serviceID, err)
			return err
		}
	}

	return nil
}

// BindGatewayHeartbeat subscribes to heartbeats sent by gateways to this service.
func (t *NatsTransport) BindGatewayHeartbeat(serviceID string, handler func(gatewayID string)) error {
	transportNatsHeartbeatDebug.Trace("Binding gateway heartbeat handler")

	subject := namespace("service", serviceID, "heartbeat")
	transportNatsHeartbeatDebug.Tracef("Subscribing to gateway heartbeats on %s", subject)

	sub, err := t.NatsConnection.Subscribe(subject, func(msg *nats.Msg) {
		payload := &HeartbeatPayload{}
		if err := msgpack.Unmarshal(msg.Data, payload); err != nil {
			transportNatsHeartbeatDebug.Tracef("Failed to unmarshal gateway heartbeat: %v", err)
			return
		}
		transportNatsHeartbeatDebug.Tracef("Received gateway heartbeat from %s", payload.ID)
		handler(payload.ID)
	})
	if err != nil {
		transportNatsHeartbeatDebug.Tracef("Failed to subscribe to gateway heartbeats: %v", err)
		return err
	}

	t.unbindGatewayHeartbeat = func() error {
		transportNatsHeartbeatDebug.Trace("Unsubscribing from gateway heartbeats")
		return sub.Unsubscribe()
	}

	transportNatsHeartbeatDebug.Trace("Gateway heartbeat handler bound successfully")
	return nil
}

// UnbindGatewayHeartbeat unsubscribes from gateway heartbeats.
func (t *NatsTransport) UnbindGatewayHeartbeat() error {
	if t.unbindGatewayHeartbeat != nil {
		return t.unbindGatewayHeartbeat()
	}
	return nil
}
