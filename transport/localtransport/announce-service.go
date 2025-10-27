package localtransport

import "github.com/RobertWHurst/eurus"

func (c *LocalTransport) AnnounceService(serviceDescriptor *eurus.ServiceDescriptor) error {
	transportLocalAnnounceDebug.Tracef("Announcing service %s with %d routes",
		serviceDescriptor.Name, len(serviceDescriptor.RouteDescriptors))

	c.mu.RLock()
	handlerCount := len(c.serviceAnnounceHandlers)
	handlers := make([]func(serviceDescriptor *eurus.ServiceDescriptor), len(c.serviceAnnounceHandlers))
	copy(handlers, c.serviceAnnounceHandlers)
	c.mu.RUnlock()

	transportLocalAnnounceDebug.Tracef("Notifying %d service announcement handlers", handlerCount)

	for _, handler := range handlers {
		handler(serviceDescriptor)
	}

	transportLocalAnnounceDebug.Trace("Service announcement completed")
	return nil
}

func (c *LocalTransport) BindServiceAnnounce(handler func(serviceDescriptor *eurus.ServiceDescriptor)) error {
	transportLocalAnnounceDebug.Trace("Binding service announcement handler")
	c.mu.Lock()
	c.serviceAnnounceHandlers = append(c.serviceAnnounceHandlers, handler)
	handlerCount := len(c.serviceAnnounceHandlers)
	c.mu.Unlock()
	transportLocalAnnounceDebug.Tracef("Now have %d service announcement handlers", handlerCount)
	return nil
}

func (c *LocalTransport) UnbindServiceAnnounce() error {
	c.mu.Lock()
	handlerCount := len(c.serviceAnnounceHandlers)
	c.serviceAnnounceHandlers = nil
	c.mu.Unlock()
	transportLocalAnnounceDebug.Tracef("Unbinding %d service announcement handlers", handlerCount)
	transportLocalAnnounceDebug.Trace("All service announcement handlers unbound")
	return nil
}
