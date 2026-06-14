package dashboard

import (
	"image"
	"sync"
)

// EPDCache stores the last rendered image for each dashboard client (identified by MAC address).
// This is used for generating partial refresh (epd_pr) diffs to minimize data transfer.
type EPDCache struct {
	mu     sync.RWMutex
	images map[string]image.Image
}

// NewEPDCache creates a new initialized EPDCache.
func NewEPDCache() *EPDCache {
	return &EPDCache{
		images: make(map[string]image.Image),
	}
}

// Get retrieves the last cached image for the given MAC address.
func (c *EPDCache) Get(mac string) image.Image {
	if mac == "" {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.images[mac]
}

// Set stores a rendered image in the cache for the given MAC address.
func (c *EPDCache) Set(mac string, img image.Image) {
	if mac == "" || img == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.images[mac] = img
}

// Global cache instance (we can define it here for simplicity)
var globalEPDCache = NewEPDCache()
