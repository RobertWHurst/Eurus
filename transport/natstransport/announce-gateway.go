package natstransport

import (
	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
	"github.com/telemetryos/go-debug/debug"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	transportNatsAnnounceDebug = debug.Bind("eurus:transport:nats:announce")
)

func (c *NatsTransport) AnnounceGateway(gatewayDescriptor *eurus.GatewayDescriptor) error {
	transportNatsAnnounceDebug.Tracef("Announcing gateway %s with %d services",
		gatewayDescriptor.Name, len(gatewayDescriptor.ServiceDescriptors))

	transportNatsAnnounceDebug.Trace("Marshaling gateway descriptor")
	descriptorBuf, err := msgpack.Marshal(gatewayDescriptor)
	if err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to marshal gateway descriptor: %v", err)
		return err
	}

	gatewayAnnounceSubject := namespace("gateway.announce")
	transportNatsAnnounceDebug.Tracef("Publishing gateway announcement to %s", gatewayAnnounceSubject)

	if err := c.NatsConnection.Publish(gatewayAnnounceSubject, descriptorBuf); err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to publish gateway announcement: %v", err)
		return err
	}

	transportNatsAnnounceDebug.Trace("Gateway announcement published successfully")
	return nil
}

func (c *NatsTransport) BindGatewayAnnounce(handler func(gatewayDescriptor *eurus.GatewayDescriptor)) error {
	transportNatsAnnounceDebug.Trace("Binding gateway announcement handler")

	subHandler := func(msg *nats.Msg) {
		transportNatsAnnounceDebug.Trace("Received gateway announcement")

		gatewayDescriptorBuf := msg.Data
		gatewayDescriptor := &eurus.GatewayDescriptor{}

		if err := msgpack.Unmarshal(gatewayDescriptorBuf, gatewayDescriptor); err != nil {
			// A malformed announcement from any peer on the bus must not crash
			// this subscriber. Log and drop it.
			transportNatsAnnounceDebug.Tracef("Failed to unmarshal gateway descriptor, dropping: %v", err)
			return
		}

		transportNatsAnnounceDebug.Tracef("Received announcement from gateway %s with %d services",
			gatewayDescriptor.Name, len(gatewayDescriptor.ServiceDescriptors))

		handler(gatewayDescriptor)
	}

	gatewayAnnounceSubject := namespace("gateway.announce")
	transportNatsAnnounceDebug.Tracef("Subscribing to gateway announcements on %s", gatewayAnnounceSubject)

	gatewayAnnounceSub, err := c.NatsConnection.Subscribe(gatewayAnnounceSubject, subHandler)
	if err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to subscribe to gateway announcements: %v", err)
		return err
	}

	transportNatsAnnounceDebug.Trace("Successfully subscribed to gateway announcements")
	c.mu.Lock()
	// Tear down any prior subscription before overwriting, so a re-bind without
	// an intervening Unbind does not leak the old subscription. The unbind
	// closure here is a non-blocking Unsubscribe, so it is safe to call while
	// holding c.mu.
	if c.unbindGatewayAnnounce != nil {
		if err := c.unbindGatewayAnnounce(); err != nil {
			transportNatsAnnounceDebug.Tracef("Failed to unbind previous gateway announce handler: %v", err)
		}
	}
	c.unbindGatewayAnnounce = func() error {
		transportNatsAnnounceDebug.Trace("Unsubscribing from gateway announcements")
		return gatewayAnnounceSub.Unsubscribe()
	}
	c.mu.Unlock()

	return nil
}

func (c *NatsTransport) UnbindGatewayAnnounce() error {
	transportNatsAnnounceDebug.Trace("Unbinding gateway announcement handler")
	c.mu.Lock()
	unbind := c.unbindGatewayAnnounce
	c.unbindGatewayAnnounce = nil
	c.mu.Unlock()
	if unbind == nil {
		return nil
	}
	err := unbind()
	if err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to unbind gateway announcement handler: %v", err)
	} else {
		transportNatsAnnounceDebug.Trace("Successfully unbound gateway announcement handler")
	}
	return err
}
