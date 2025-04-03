package natstransport

import (
	"github.com/RobertWHurst/eurus"
	"github.com/nats-io/nats.go"
	"github.com/vmihailenco/msgpack/v5"
)

func (c *NatsTransport) AnnounceService(serviceDescriptor *eurus.ServiceDescriptor) error {
	serviceDescriptorBuf, err := msgpack.Marshal(serviceDescriptor)
	if err != nil {
		return err
	}
	return c.NatsConnection.Publish(namespace("service.announce"), serviceDescriptorBuf)
}

func (c *NatsTransport) BindServiceAnnounce(handler func(serviceDescriptor *eurus.ServiceDescriptor)) error {
	subHandler := func(msg *nats.Msg) {
		serviceDescriptorBuf := msg.Data
		serviceDescriptor := &eurus.ServiceDescriptor{}

		if err := msgpack.Unmarshal(serviceDescriptorBuf, serviceDescriptor); err != nil {
			panic(err)
		}

		handler(serviceDescriptor)
	}

	serviceAnnounceSub, err := c.NatsConnection.Subscribe(namespace("service.announce"), subHandler)
	if err != nil {
		return err
	}
	c.unbindServiceAnnounce = func() error {
		return serviceAnnounceSub.Unsubscribe()
	}

	return nil
}

func (c *NatsTransport) UnbindServiceAnnounce() error {
	return c.unbindServiceAnnounce()
}
