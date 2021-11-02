package exporter

import (
	v1 "k8s.io/api/core/v1"
	"sync"
)

type ContainerCache interface {
	AddContainer(pod *v1.Pod)
	DelContainer(UID string)
	GetContainerName(UID string, containerID string) (string, bool)
	GetContainer(UID string) (map[string]string, bool)

}

type containerCache struct {
	cache   map[string]map[string]string
	mu      sync.Mutex

}

func (c *containerCache) AddContainer(pod *v1.Pod){
	if c.cache[string(pod.UID)] == nil {
		c.cache[string(pod.UID)] = make(map[string]string)
	}
	for _, container := range pod.Status.ContainerStatuses{
		c.cache[string(pod.UID)][container.ContainerID] = container.Name
	}
}

func (c *containerCache) DelContainer(UID string){
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, UID)
}

func (c *containerCache) GetContainerName(UID string, containerID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	containerName, exist := c.cache[UID][containerID]
	return containerName, exist
}

func (c *containerCache) GetContainer(UID string) (map[string]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	containerMap, exist := c.cache[UID]
	return containerMap, exist
}

func NewContCache() ContainerCache {
	return &containerCache{
		cache: make(map[string]map[string]string),
		mu:    sync.Mutex{},
	}
}