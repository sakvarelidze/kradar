package cache

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

type HTTPCache struct {
	client *http.Client
	ttl    time.Duration
	mu     sync.RWMutex
	items  map[string]entry
}

type entry struct {
	body      []byte
	expiresAt time.Time
}

func NewHTTPCache(timeout, ttl time.Duration) *HTTPCache {
	return &HTTPCache{
		client: &http.Client{Timeout: timeout},
		ttl:    ttl,
		items:  make(map[string]entry),
	}
}

func (c *HTTPCache) Get(ctx context.Context, url string) ([]byte, error) {
	c.mu.RLock()
	cached, ok := c.items[url]
	if ok && time.Now().Before(cached.expiresAt) {
		c.mu.RUnlock()
		return cached.body, nil
	}
	c.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.items[url] = entry{body: body, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return body, nil
}
