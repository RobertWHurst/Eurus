package localtransport

import (
	"context"

	"github.com/RobertWHurst/eurus"
)

func (c *LocalTransport) AnnounceService(serviceDescriptor *eurus.ServiceDescriptor) error {
	transportLocalAnnounceDebug.Tracef("Announcing service %s with %d routes",
		serviceDescriptor.Name, len(serviceDescriptor.RouteDescriptors))

	c.mu.RLock()
	handlerCount := len(c.serviceAnnounceHandlers)
	handlers := make([]func(serviceDescriptor *eurus.ServiceDescriptor), 0, len(c.serviceAnnounceHandlers))
	for _, handler := range c.serviceAnnounceHandlers {
		handlers = append(handlers, handler)
	}
	c.mu.RUnlock()

	transportLocalAnnounceDebug.Tracef("Notifying %d service announcement handlers", handlerCount)

	for _, handler := range handlers {
		handler(serviceDescriptor)
	}

	transportLocalAnnounceDebug.Trace("Service announcement completed")
	return nil
}

func (c *LocalTransport) HandleServiceAnnouncements(ctx context.Context, ready chan<- struct{}, handler func(serviceDescriptor *eurus.ServiceDescriptor)) error {
	transportLocalAnnounceDebug.Trace("Handling service announcements")
	c.mu.Lock()
	id := c.registerHandler()
	c.serviceAnnounceHandlers[id] = handler
	handlerCount := len(c.serviceAnnounceHandlers)
	c.mu.Unlock()
	transportLocalAnnounceDebug.Tracef("Now have %d service announcement handlers", handlerCount)
	signalReady(ready)

	<-ctx.Done()
	c.mu.Lock()
	handlerCount = len(c.serviceAnnounceHandlers)
	delete(c.serviceAnnounceHandlers, id)
	c.mu.Unlock()
	transportLocalAnnounceDebug.Tracef("Unbinding %d service announcement handlers", handlerCount)
	return nil
}
