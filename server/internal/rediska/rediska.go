package rediska

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/nalgeon/redka"
)

type Cache struct {
	db  *redka.DB
	log *slog.Logger
	mu  sync.RWMutex
}

func Open(path string) (*Cache, error) {
	opts := redka.Options{
		DriverName: "sqlite",
	}
	db, err := redka.Open(path, &opts)
	if err != nil {
		return nil, err
	}
	return &Cache{
		db:  db,
		log: slog.Default(),
	}, nil
}

func OpenMemory() (*Cache, error) {
	db, err := redka.Open(":memory:", nil)
	if err != nil {
		return nil, err
	}
	return &Cache{
		db:  db,
		log: slog.Default(),
	}, nil
}

func (c *Cache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *Cache) Get(ctx context.Context, key string, dest any) (bool, error) {
	if c.db == nil {
		return false, nil
	}
	val, err := c.db.Str().Get(key)
	if err != nil || val.String() == "" {
		return false, err
	}
	if err := json.Unmarshal([]byte(val.String()), dest); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if c.db == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := c.db.Str().Set(key, string(data)); err != nil {
		return err
	}
	if ttl > 0 {
		_ = c.db.Key().Expire(key, ttl)
	}
	return nil
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	if c.db == nil {
		return nil
	}
	_, err := c.db.Key().Delete(key)
	return err
}