package localtransport

import (
	"context"

	"github.com/RobertWHurst/eurus"
)

func (t *LocalTransport) AnnounceGateway(gatewayDescriptor *eurus.GatewayDescriptor) error {
	transportLocalAnnounceDebug.Tracef("Announcing gateway %s with %d services",
		gatewayDescriptor.Name, len(gatewayDescriptor.ServiceDescriptors))

	t.mu.RLock()
	handlers := make([]func(gatewayDescriptor *eurus.GatewayDescriptor), 0, len(t.gatewayAnnounceHandlers))
	for _, handler := range t.gatewayAnnounceHandlers {
		handlers = append(handlers, handler)
	}
	t.mu.RUnlock()

	transportLocalAnnounceDebug.Tracef("Notifying %d gateway announcement handlers", len(handlers))
	for _, handler := range handlers {
		handler(gatewayDescriptor)
	}

	return nil
}

func (t *LocalTransport) SubscribeGatewayAnnouncements(_ context.Context, handler func(gatewayDescriptor *eurus.GatewayDescriptor)) (eurus.Subscription, error) {
	transportLocalAnnounceDebug.Trace("Subscribing to gateway announcements")
	t.mu.Lock()
	id := t.registerHandlerID()
	t.gatewayAnnounceHandlers[id] = handler
	t.mu.Unlock()

	return untilDone(func() {
		transportLocalAnnounceDebug.Trace("Unsubscribing from gateway announcements")
		t.mu.Lock()
		delete(t.gatewayAnnounceHandlers, id)
		t.mu.Unlock()
	}), nil
}
