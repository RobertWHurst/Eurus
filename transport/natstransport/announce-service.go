package natstransport

import (
	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

func (c *NatsTransport) AnnounceService(serviceDescriptor *eurus.ServiceDescriptor) error {
	transportNatsAnnounceDebug.Tracef("Announcing service %s with %d routes",
		serviceDescriptor.Name, len(serviceDescriptor.RouteDescriptors))

	transportNatsAnnounceDebug.Trace("Marshaling service descriptor")
	serviceDescriptorBuf, err := msgpack.Marshal(serviceDescriptor)
	if err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to marshal service descriptor: %v", err)
		return err
	}

	serviceAnnounceSubject := namespace("service.announce")
	transportNatsAnnounceDebug.Tracef("Publishing service announcement to %s", serviceAnnounceSubject)

	if err := c.NatsConnection.Publish(serviceAnnounceSubject, serviceDescriptorBuf); err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to publish service announcement: %v", err)
		return err
	}

	transportNatsAnnounceDebug.Trace("Service announcement published successfully")
	return nil
}

func (c *NatsTransport) BindServiceAnnounce(handler func(serviceDescriptor *eurus.ServiceDescriptor)) error {
	transportNatsAnnounceDebug.Trace("Binding service announcement handler")

	subHandler := func(msg *nats.Msg) {
		transportNatsAnnounceDebug.Trace("Received service announcement")

		serviceDescriptorBuf := msg.Data
		serviceDescriptor := &eurus.ServiceDescriptor{}

		if err := msgpack.Unmarshal(serviceDescriptorBuf, serviceDescriptor); err != nil {
			// A malformed announcement from any peer on the bus must not crash
			// this subscriber. Log and drop it.
			transportNatsAnnounceDebug.Tracef("Failed to unmarshal service descriptor, dropping: %v", err)
			return
		}

		transportNatsAnnounceDebug.Tracef("Received announcement from service %s with %d routes",
			serviceDescriptor.Name, len(serviceDescriptor.RouteDescriptors))

		handler(serviceDescriptor)
	}

	serviceAnnounceSubject := namespace("service.announce")
	transportNatsAnnounceDebug.Tracef("Subscribing to service announcements on %s", serviceAnnounceSubject)

	serviceAnnounceSub, err := c.NatsConnection.Subscribe(serviceAnnounceSubject, subHandler)
	if err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to subscribe to service announcements: %v", err)
		return err
	}

	transportNatsAnnounceDebug.Trace("Successfully subscribed to service announcements")
	c.mu.Lock()
	// Tear down any prior subscription before overwriting, so a re-bind without
	// an intervening Unbind does not leak the old subscription. The unbind
	// closure here is a non-blocking Unsubscribe, so it is safe to call while
	// holding c.mu.
	if c.unbindServiceAnnounce != nil {
		if err := c.unbindServiceAnnounce(); err != nil {
			transportNatsAnnounceDebug.Tracef("Failed to unbind previous service announce handler: %v", err)
		}
	}
	c.unbindServiceAnnounce = func() error {
		transportNatsAnnounceDebug.Trace("Unsubscribing from service announcements")
		return serviceAnnounceSub.Unsubscribe()
	}
	c.mu.Unlock()

	return nil
}

func (c *NatsTransport) UnbindServiceAnnounce() error {
	transportNatsAnnounceDebug.Trace("Unbinding service announcement handler")
	c.mu.Lock()
	unbind := c.unbindServiceAnnounce
	c.unbindServiceAnnounce = nil
	c.mu.Unlock()
	if unbind == nil {
		return nil
	}
	err := unbind()
	if err != nil {
		transportNatsAnnounceDebug.Tracef("Failed to unbind service announcement handler: %v", err)
	} else {
		transportNatsAnnounceDebug.Trace("Successfully unbound service announcement handler")
	}
	return err
}
