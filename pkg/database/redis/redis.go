package redis

import (
	"p9e.in/samavaya/packages/api/v1/config"

	"github.com/go-redis/redis/v8"
)

// provideRedisClient creates a Redis client.
func ProvideRedisClient(c *config.Data) (*redis.Client, func(), error) {
	var password = ""
	db := int(0)
	if c.Redis.Password != nil {
		// Access the Value field to get the actual string value
		password = c.Redis.Password.Value
	}
	if c.Redis.Db != nil {
		db = int(c.Redis.Db.Value)
	}
	client := redis.NewClient(&redis.Options{
		Addr:     c.Redis.Addr, // Update with your Redis server address
		Password: password,     // No password
		DB:       db,           // Default DB
	})

	return client, func() {
		client.Close()
	}, nil
}
