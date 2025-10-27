package localtransport

import "github.com/RobertWHurst/eurus"

func (c *LocalTransport) AnnounceGateway(gatewayDescriptor *eurus.GatewayDescriptor) error {
	transportLocalAnnounceDebug.Tracef("Announcing gateway %s with %d services",
		gatewayDescriptor.Name, len(gatewayDescriptor.ServiceDescriptors))

	c.mu.RLock()
	handlerCount := len(c.gatewayAnnounceHandlers)
	handlers := make([]func(gatewayDescriptor *eurus.GatewayDescriptor), len(c.gatewayAnnounceHandlers))
	copy(handlers, c.gatewayAnnounceHandlers)
	c.mu.RUnlock()

	transportLocalAnnounceDebug.Tracef("Notifying %d gateway announcement handlers", handlerCount)

	for _, handler := range handlers {
		handler(gatewayDescriptor)
	}

	transportLocalAnnounceDebug.Trace("Gateway announcement completed")
	return nil
}

func (c *LocalTransport) BindGatewayAnnounce(handler func(gatewayDescriptor *eurus.GatewayDescriptor)) error {
	transportLocalAnnounceDebug.Trace("Binding gateway announcement handler")
	c.mu.Lock()
	c.gatewayAnnounceHandlers = append(c.gatewayAnnounceHandlers, handler)
	handlerCount := len(c.gatewayAnnounceHandlers)
	c.mu.Unlock()
	transportLocalAnnounceDebug.Tracef("Now have %d gateway announcement handlers", handlerCount)
	return nil
}

func (c *LocalTransport) UnbindGatewayAnnounce() error {
	c.mu.Lock()
	handlerCount := len(c.gatewayAnnounceHandlers)
	c.gatewayAnnounceHandlers = nil
	c.mu.Unlock()
	transportLocalAnnounceDebug.Tracef("Unbinding %d gateway announcement handlers", handlerCount)
	transportLocalAnnounceDebug.Trace("All gateway announcement handlers unbound")
	return nil
}
