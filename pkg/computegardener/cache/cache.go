package cache

import (
	"sync"
	"time"

	"k8s.io/klog/v2"
	"github.com/elevated-systems/compute-gardener-scheduler/pkg/computegardener/api"
)

// Cache provides thread-safe caching of electricity data with TTL
type Cache struct {
	data    map[string]*cacheEntry
	mutex   sync.RWMutex
	ttl     time.Duration
	maxAge  time.Duration
	stopCh  chan struct{}
	metrics *metrics
}

type cacheEntry struct {
	data      *api.ElectricityData
	timestamp time.Time
	hits      int64
}

type metrics struct {
	hits   int64
	misses int64
	mutex  sync.RWMutex
}

// New creates a new cache instance
func New(ttl time.Duration, maxAge time.Duration) *Cache {
	c := &Cache{
		data: make(map[string]*cacheEntry),
		// For cache freshness purposes at get time.
		ttl: ttl,
		// Age to clean-up unaccessed items.
		maxAge:  maxAge,
		stopCh:  make(chan struct{}),
		metrics: &metrics{},
	}

	// Start cleanup goroutine
	go c.cleanup()

	return c
}

// Get retrieves data from cache if valid
func (c *Cache) Get(region string) (*api.ElectricityData, bool) {
	c.mutex.RLock()
	entry, exists := c.data[region]
	c.mutex.RUnlock()

	if !exists {
		c.recordMiss()
		return nil, false
	}

	age := time.Since(entry.timestamp)
	if age > c.ttl {
		c.recordMiss()
		return nil, false
	}

	// Update metrics under write lock
	c.mutex.Lock()
	entry.hits++
	c.recordHit()
	c.mutex.Unlock()

	return entry.data, true
}

// Set stores data in cache
func (c *Cache) Set(region string, data *api.ElectricityData) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.data[region] = &cacheEntry{
		data:      data,
		timestamp: time.Now(),
		hits:      0,
	}

	klog.V(4).InfoS("Cached electricity data",
		"region", region,
		"carbonIntensity", data.CarbonIntensity,
		"timestamp", data.Timestamp)
}

// GetMetrics returns cache performance metrics
func (c *Cache) GetMetrics() (hits, misses int64) {
	c.metrics.mutex.RLock()
	defer c.metrics.mutex.RUnlock()
	return c.metrics.hits, c.metrics.misses
}

func (c *Cache) recordHit() {
	c.metrics.mutex.Lock()
	c.metrics.hits++
	c.metrics.mutex.Unlock()
}

func (c *Cache) recordMiss() {
	c.metrics.mutex.Lock()
	c.metrics.misses++
	c.metrics.mutex.Unlock()
}

// cleanup periodically removes expired entries
func (c *Cache) cleanup() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.removeExpired()
		}
	}
}

func (c *Cache) removeExpired() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	now := time.Now()
	for region, entry := range c.data {
		age := now.Sub(entry.timestamp)
		if age > c.maxAge {
			delete(c.data, region)
			klog.V(4).InfoS("Removed expired cache entry",
				"region", region,
				"age", age.String(),
				"hits", entry.hits)
		}
	}
}

// Close stops the cleanup goroutine
func (c *Cache) Close() {
	close(c.stopCh)
}

// Clear removes all entries from the cache
func (c *Cache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.data = make(map[string]*cacheEntry)
	klog.V(4).Info("Cleared cache")
}

// Size returns the number of entries in the cache
func (c *Cache) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.data)
}

// GetRegions returns a list of cached regions
func (c *Cache) GetRegions() []string {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	regions := make([]string, 0, len(c.data))
	for region := range c.data {
		regions = append(regions, region)
	}
	return regions
}
