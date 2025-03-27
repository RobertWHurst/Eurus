package eurus

type GatewayServiceIndexer struct {
	ServiceDescriptors []*ServiceDescriptor
}

func (r *GatewayServiceIndexer) SetServiceDescriptor(descriptor *ServiceDescriptor) error {
	for _, existingDescriptor := range r.ServiceDescriptors {
		if existingDescriptor.Name == descriptor.Name {
			existingDescriptor.RouteDescriptors = descriptor.RouteDescriptors
			return nil
		}
	}
	r.ServiceDescriptors = append(r.ServiceDescriptors, descriptor)
	return nil
}

func (r *GatewayServiceIndexer) UnsetService(name string) error {
	for i, service := range r.ServiceDescriptors {
		if service.Name == name {
			r.ServiceDescriptors = append(r.ServiceDescriptors[:i], r.ServiceDescriptors[i+1:]...)
			return nil
		}
	}
	return nil
}

func (r *GatewayServiceIndexer) ResolveService(path string) (string, bool) {
	for _, remoteService := range r.ServiceDescriptors {
		if remoteService.UnreachableAt != nil {
			continue
		}
		for _, httpRoute := range remoteService.RouteDescriptors {
			if _, isMatch := httpRoute.Pattern.Match(path); isMatch {
				return remoteService.Name, true
			}
		}
	}
	return "", false
}
