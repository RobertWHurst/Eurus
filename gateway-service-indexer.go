package eurus

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

type GatewayServiceIndexer struct {
	mu               sync.Mutex
	closed           bool
	descriptors      []*ServiceDescriptor
	servicesByName   map[string][]*ServiceDescriptor
	socketToInstance map[string]map[string]string
}

func NewGatewayServiceIndexer() *GatewayServiceIndexer {
	return &GatewayServiceIndexer{
		descriptors:      []*ServiceDescriptor{},
		servicesByName:   map[string][]*ServiceDescriptor{},
		socketToInstance: map[string]map[string]string{},
	}
}

func (r *GatewayServiceIndexer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.descriptors = nil
	r.servicesByName = nil
	r.socketToInstance = nil
}

func (r *GatewayServiceIndexer) IsClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *GatewayServiceIndexer) SetServiceDescriptor(descriptor *ServiceDescriptor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	now := time.Now()

	instances := r.servicesByName[descriptor.Name]
	for _, existing := range instances {
		if existing.ID == descriptor.ID {
			existing.RouteDescriptors = descriptor.RouteDescriptors
			existing.LastSeenAt = &now
			return nil
		}
	}

	descriptor.LastSeenAt = &now
	r.servicesByName[descriptor.Name] = append(instances, descriptor)
	r.descriptors = append(r.descriptors, descriptor)

	return nil
}

func (r *GatewayServiceIndexer) UnsetService(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	var serviceName string
	for name, instances := range r.servicesByName {
		i := slices.IndexFunc(instances, func(d *ServiceDescriptor) bool { return d.ID == id })
		if i != -1 {
			serviceName = name
			r.servicesByName[name] = slices.Delete(instances, i, i+1)
			if len(r.servicesByName[name]) == 0 {
				delete(r.servicesByName, name)
			}
			break
		}
	}
	for i, descriptor := range r.descriptors {
		if descriptor.ID == id {
			r.descriptors = slices.Delete(r.descriptors, i, i+1)
			break
		}
	}

	if serviceName == "" {
		return nil
	}

	for socketID, services := range r.socketToInstance {
		if instanceID, ok := services[serviceName]; ok && instanceID == id {
			delete(services, serviceName)
			if len(services) == 0 {
				delete(r.socketToInstance, socketID)
			}
		}
	}

	return nil
}

func (r *GatewayServiceIndexer) FreshServiceDescriptors(threshold time.Duration) []*ServiceDescriptor {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	now := time.Now()
	var fresh []*ServiceDescriptor
	for _, d := range r.descriptors {
		if d.LastSeenAt != nil && now.Sub(*d.LastSeenAt) <= threshold {
			fresh = append(fresh, d)
		}
	}
	return fresh
}

func (r *GatewayServiceIndexer) PruneStaleServices(threshold time.Duration) ([]string, []string) {
	r.mu.Lock()

	if r.closed {
		r.mu.Unlock()
		return nil, nil
	}

	var staleIDs []string
	var affectedSocketIDs []string
	now := time.Now()

	for _, d := range r.descriptors {
		if d.LastSeenAt == nil || now.Sub(*d.LastSeenAt) > threshold {
			staleIDs = append(staleIDs, d.ID)
		}
	}

	// Collect affected socket IDs before removing services
	for _, staleID := range staleIDs {
		for socketID, services := range r.socketToInstance {
			for _, instanceID := range services {
				if instanceID == staleID {
					affectedSocketIDs = append(affectedSocketIDs, socketID)
					break
				}
			}
		}
	}

	r.mu.Unlock()

	for _, id := range staleIDs {
		r.UnsetService(id)
	}

	return staleIDs, affectedSocketIDs
}

func (r *GatewayServiceIndexer) ResolveService(path string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return "", false
	}

	for serviceName, instances := range r.servicesByName {
		for _, instance := range instances {
			for _, route := range instance.RouteDescriptors {
				if _, isMatch := route.Pattern.Match(path); isMatch {
					return serviceName, true
				}
			}
		}
	}
	return "", false
}

func (r *GatewayServiceIndexer) MapSocket(serviceName, socketID string) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return "", false, nil
	}

	if services, ok := r.socketToInstance[socketID]; ok {
		if instanceID, ok := services[serviceName]; ok {
			return instanceID, false, nil
		}
	}

	instances, ok := r.servicesByName[serviceName]
	if !ok || len(instances) == 0 {
		return "", false, nil
	}

	var targetInstance *ServiceDescriptor
	for _, instance := range instances {
		if targetInstance == nil ||
			len(targetInstance.socketIDs) > len(instance.socketIDs) {
			targetInstance = instance
		}
	}
	if targetInstance == nil {
		return "", false, fmt.Errorf("service has no reachable instances: %s", serviceName)
	}

	targetInstance.socketIDs = append(targetInstance.socketIDs, socketID)

	if _, ok := r.socketToInstance[socketID]; !ok {
		r.socketToInstance[socketID] = make(map[string]string)
	}
	r.socketToInstance[socketID][serviceName] = targetInstance.ID

	return targetInstance.ID, true, nil
}

func (r *GatewayServiceIndexer) UnmapSocket(socketID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return
	}

	services, ok := r.socketToInstance[socketID]
	if !ok {
		return
	}

	for serviceName, instanceID := range services {
		if instances, ok := r.servicesByName[serviceName]; ok {
			for _, instance := range instances {
				if instance.ID == instanceID {
					i := slices.Index(instance.socketIDs, socketID)
					if i != -1 {
						instance.socketIDs = slices.Delete(instance.socketIDs, i, i+1)
					}
					break
				}
			}
		}
	}

	delete(r.socketToInstance, socketID)
}
