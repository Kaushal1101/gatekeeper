package redis

import (
	"os"

	goredis "github.com/redis/go-redis/v9"
)

func NewClient() *goredis.Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	return goredis.NewClient(&goredis.Options{
		Addr: addr,
	})
}
