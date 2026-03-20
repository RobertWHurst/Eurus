package eurus

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGatewayServiceIndexer_SetServiceDescriptor_AddsNewService(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	err := indexer.SetServiceDescriptor(descriptor1)
	assert.NoError(t, err)
	assert.Len(t, indexer.descriptors, 1)
	assert.Len(t, indexer.servicesByName["user-service"], 1)
	assert.Equal(t, "instance-1", indexer.servicesByName["user-service"][0].ID)
}

func TestGatewayServiceIndexer_SetServiceDescriptor_SetsLastSeenAt(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	descriptor := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route},
	}

	before := time.Now()
	indexer.SetServiceDescriptor(descriptor)

	assert.NotNil(t, indexer.descriptors[0].LastSeenAt)
	assert.False(t, indexer.descriptors[0].LastSeenAt.Before(before))
}

func TestGatewayServiceIndexer_SetServiceDescriptor_UpdatesExistingService(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)

	route2, _ := NewRouteDescriptor("/api/users/:id")
	descriptor2 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route2},
	}

	err := indexer.SetServiceDescriptor(descriptor2)
	assert.NoError(t, err)
	assert.Len(t, indexer.descriptors, 1)
	assert.Len(t, indexer.servicesByName["user-service"], 1)
	assert.Len(t, indexer.servicesByName["user-service"][0].RouteDescriptors, 1)
	assert.Equal(t, "/api/users/:id", indexer.servicesByName["user-service"][0].RouteDescriptors[0].Pattern.String())
}

func TestGatewayServiceIndexer_SetServiceDescriptor_UpdatesLastSeenAt(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	descriptor := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route},
	}

	indexer.SetServiceDescriptor(descriptor)
	first := *indexer.descriptors[0].LastSeenAt

	time.Sleep(time.Millisecond)
	indexer.SetServiceDescriptor(descriptor)
	second := *indexer.descriptors[0].LastSeenAt

	assert.True(t, second.After(first))
}

func TestGatewayServiceIndexer_SetServiceDescriptor_AddsMultipleInstances(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	descriptor2 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-2",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)
	indexer.SetServiceDescriptor(descriptor2)

	assert.Len(t, indexer.descriptors, 2)
	assert.Len(t, indexer.servicesByName["user-service"], 2)
}

func TestGatewayServiceIndexer_UnsetService_RemovesService(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)

	err := indexer.UnsetService("instance-1")
	assert.NoError(t, err)
	assert.Len(t, indexer.descriptors, 0)
	assert.Len(t, indexer.servicesByName, 0)
}

func TestGatewayServiceIndexer_UnsetService_RemovesOneInstanceFromMultiple(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	descriptor2 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-2",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)
	indexer.SetServiceDescriptor(descriptor2)

	err := indexer.UnsetService("instance-1")
	assert.NoError(t, err)
	assert.Len(t, indexer.descriptors, 1)
	assert.Len(t, indexer.servicesByName["user-service"], 1)
	assert.Equal(t, "instance-2", indexer.servicesByName["user-service"][0].ID)
}

func TestGatewayServiceIndexer_UnsetService_CleansUpSocketMappings(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)
	indexer.MapSocket("user-service", "socket-1")

	err := indexer.UnsetService("instance-1")
	assert.NoError(t, err)
	assert.Len(t, indexer.socketToInstance, 0)
}

func TestGatewayServiceIndexer_ResolveService_FindsMatchingService(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)

	serviceName, found := indexer.ResolveService("/api/users")
	assert.True(t, found)
	assert.Equal(t, "user-service", serviceName)
}

func TestGatewayServiceIndexer_ResolveService_MatchesPathParameters(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users/:id")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)

	serviceName, found := indexer.ResolveService("/api/users/123")
	assert.True(t, found)
	assert.Equal(t, "user-service", serviceName)
}

func TestGatewayServiceIndexer_ResolveService_ReturnsNotFoundForUnmatchedPath(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)

	_, found := indexer.ResolveService("/api/posts")
	assert.False(t, found)
}

func TestGatewayServiceIndexer_MapSocket_CreatesNewMapping(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)

	instanceID, isNew, err := indexer.MapSocket("user-service", "socket-1")
	assert.NoError(t, err)
	assert.True(t, isNew)
	assert.Equal(t, "instance-1", instanceID)
	assert.Len(t, indexer.socketToInstance["socket-1"], 1)
	assert.Equal(t, "instance-1", indexer.socketToInstance["socket-1"]["user-service"])
}

func TestGatewayServiceIndexer_MapSocket_ReturnsExistingMapping(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)

	instanceID1, isNew1, _ := indexer.MapSocket("user-service", "socket-1")
	assert.True(t, isNew1)
	instanceID2, isNew2, _ := indexer.MapSocket("user-service", "socket-1")
	assert.False(t, isNew2)

	assert.Equal(t, instanceID1, instanceID2)
	assert.Len(t, descriptor1.socketIDs, 1)
}

func TestGatewayServiceIndexer_MapSocket_BalancesLoadAcrossInstances(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	descriptor2 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-2",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)
	indexer.SetServiceDescriptor(descriptor2)

	// Map 100 sockets to verify load balancing distributes evenly
	// Load balancing algorithm picks instance with fewest connections
	instanceCounts := make(map[string]int)
	for i := 0; i < 100; i++ {
		socketID := fmt.Sprintf("socket-%d", i)
		instanceID, isNew, err := indexer.MapSocket("user-service", socketID)
		assert.NoError(t, err)
		assert.True(t, isNew)
		assert.NotEmpty(t, instanceID)
		assert.True(t, instanceID == "instance-1" || instanceID == "instance-2")
		instanceCounts[instanceID]++
	}

	// Should distribute evenly (50/50)
	assert.Equal(t, 50, instanceCounts["instance-1"], "instance-1 should have 50 connections")
	assert.Equal(t, 50, instanceCounts["instance-2"], "instance-2 should have 50 connections")

	// Verify least-loaded algorithm: if we unmap one socket from instance-1,
	// the next socket should go to instance-1 (now has 49, instance-2 has 50)
	indexer.UnmapSocket("socket-0") // This was on instance-1
	newInstanceID, isNew, err := indexer.MapSocket("user-service", "socket-new")
	assert.NoError(t, err)
	assert.True(t, isNew)
	assert.Equal(t, "instance-1", newInstanceID, "Should pick instance with fewer connections")
}

func TestGatewayServiceIndexer_MapSocket_ReturnsEmptyForUnknownService(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	serviceID, isNew, err := indexer.MapSocket("unknown-service", "socket-1")
	assert.NoError(t, err)
	assert.Equal(t, "", serviceID, "Should return empty string for unknown service")
	assert.False(t, isNew, "isNew should be false when service not found")
}

func TestGatewayServiceIndexer_UnmapSocket_RemovesMapping(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	indexer.SetServiceDescriptor(descriptor1)
	indexer.MapSocket("user-service", "socket-1")

	indexer.UnmapSocket("socket-1")

	assert.Len(t, indexer.socketToInstance, 0)
	assert.Len(t, descriptor1.socketIDs, 0)
}

func TestGatewayServiceIndexer_UnmapSocket_RemovesMappingsForMultipleServices(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route1, _ := NewRouteDescriptor("/api/users")
	descriptor1 := &ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route1},
	}

	route2, _ := NewRouteDescriptor("/api/posts")
	descriptor2 := &ServiceDescriptor{
		Name:             "post-service",
		ID:               "instance-2",
		RouteDescriptors: []*RouteDescriptor{route2},
	}

	indexer.SetServiceDescriptor(descriptor1)
	indexer.SetServiceDescriptor(descriptor2)
	indexer.MapSocket("user-service", "socket-1")
	indexer.MapSocket("post-service", "socket-1")

	indexer.UnmapSocket("socket-1")

	assert.Len(t, indexer.socketToInstance, 0)
	assert.Len(t, descriptor1.socketIDs, 0)
	assert.Len(t, descriptor2.socketIDs, 0)
}

func TestGatewayServiceIndexer_UnmapSocket_HandlesNonexistentSocket(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	indexer.UnmapSocket("nonexistent-socket")

	assert.Len(t, indexer.socketToInstance, 0)
}

func TestGatewayServiceIndexer_FreshServiceDescriptors_ReturnsFreshOnly(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "user-service",
		ID:               "stale-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	time.Sleep(5 * time.Millisecond)

	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "user-service",
		ID:               "fresh-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	fresh := indexer.FreshServiceDescriptors(2 * time.Millisecond)
	assert.Len(t, fresh, 1)
	assert.Equal(t, "fresh-1", fresh[0].ID)
}

func TestGatewayServiceIndexer_FreshServiceDescriptors_ReturnsAllWhenAllFresh(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "svc-a",
		ID:               "id-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "svc-b",
		ID:               "id-2",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	fresh := indexer.FreshServiceDescriptors(time.Hour)
	assert.Len(t, fresh, 2)
}

func TestGatewayServiceIndexer_FreshServiceDescriptors_ReturnsNilWhenClosed(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "svc-a",
		ID:               "id-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	indexer.Close()

	fresh := indexer.FreshServiceDescriptors(time.Hour)
	assert.Nil(t, fresh)
}

func TestGatewayServiceIndexer_PruneStaleServices_RemovesStaleService(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	time.Sleep(5 * time.Millisecond)

	prunedIDs, _ := indexer.PruneStaleServices(time.Millisecond)

	assert.Len(t, indexer.descriptors, 0)
	assert.Len(t, indexer.servicesByName, 0)
	assert.Contains(t, prunedIDs, "instance-1")
}

func TestGatewayServiceIndexer_PruneStaleServices_KeepsFreshService(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	prunedIDs, _ := indexer.PruneStaleServices(time.Hour)

	assert.Len(t, indexer.descriptors, 1)
	assert.Len(t, indexer.servicesByName["user-service"], 1)
	assert.Empty(t, prunedIDs)
}

func TestGatewayServiceIndexer_PruneStaleServices_PrunesOnlyStale(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "user-service",
		ID:               "stale-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	time.Sleep(5 * time.Millisecond)

	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "user-service",
		ID:               "fresh-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	prunedIDs, _ := indexer.PruneStaleServices(2 * time.Millisecond)

	assert.Len(t, indexer.descriptors, 1)
	assert.Equal(t, "fresh-1", indexer.descriptors[0].ID)
	assert.Contains(t, prunedIDs, "stale-1")
	assert.NotContains(t, prunedIDs, "fresh-1")
}

func TestGatewayServiceIndexer_PruneStaleServices_ReturnsAffectedSocketIDs(t *testing.T) {
	indexer := NewGatewayServiceIndexer()

	route, _ := NewRouteDescriptor("/api/users")
	indexer.SetServiceDescriptor(&ServiceDescriptor{
		Name:             "user-service",
		ID:               "instance-1",
		RouteDescriptors: []*RouteDescriptor{route},
	})

	indexer.MapSocket("user-service", "socket-1")
	indexer.MapSocket("user-service", "socket-2")
	time.Sleep(5 * time.Millisecond)

	_, affectedSocketIDs := indexer.PruneStaleServices(time.Millisecond)

	assert.Len(t, indexer.socketToInstance, 0)
	assert.Contains(t, affectedSocketIDs, "socket-1")
	assert.Contains(t, affectedSocketIDs, "socket-2")
}

func TestGatewayServiceIndexer_PruneStaleServices_SafeWhenClosed(t *testing.T) {
	indexer := NewGatewayServiceIndexer()
	indexer.Close()

	assert.NotPanics(t, func() {
		indexer.PruneStaleServices(time.Millisecond)
	})
}
