package cache

import (
	"sync/atomic"

	lru "github.com/hashicorp/golang-lru"
)

type Cache struct {
	*lru.ARCCache

	hit, miss uint32
}

type Stats struct {
	Hit  uint32 `json:"hit"`
	Miss uint32 `json:"miss"`
}

func New(size int) (*Cache, error) {
	arc, err := lru.NewARC(size)
	if err != nil {
		return nil, err
	}
	return &Cache{ARCCache: arc}, nil
}

func (c *Cache) Get(key interface{}) (value interface{}, ok bool) {
	value, ok = c.ARCCache.Get(key)
	if ok {
		atomic.AddUint32(&c.hit, 1)
	} else {
		atomic.AddUint32(&c.miss, 1)
	}
	return value, ok
}

func (c *Cache) Stats() Stats {
	return Stats{
		Hit:  atomic.LoadUint32(&c.hit),
		Miss: atomic.LoadUint32(&c.miss),
	}
}
