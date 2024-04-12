package rcache

import (
	"context"
	"github.com/redis/go-redis/v9"
	"time"
)

type Cache struct {
	client *redis.Client
	maxAge time.Duration
}

func NewCache(addr, password string, db int, maxAge time.Duration) *Cache {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return &Cache{
		client: client,
		maxAge: maxAge,
	}
}

func (c *Cache) Ping(ctx context.Context) error {
	pong, err := c.client.Ping(ctx).Result()
	if err != nil {
		return err
	}
	println(pong)
	return nil
}

func (c *Cache) Add(key, value interface{}) {
	c.client.Set(context.Background(), key.(string), value, c.maxAge)
}

func (c *Cache) Get(key interface{}) (interface{}, bool) {
	val, err := c.client.Get(context.Background(), key.(string)).Result()
	if err == redis.Nil {
		return nil, false
	} else if err != nil {
		// Handle other errors
		return nil, false
	}

	return val, true
}

func (c *Cache) Keys() []string {
	keys, err := c.client.Keys(context.Background(), "*").Result()
	if err != nil {
		return keys
	}
	return keys
}
