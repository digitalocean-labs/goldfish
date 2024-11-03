package main

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gomodule/redigo/redis"
	"gotest.tools/v3/assert"
)

func TestRedisRoundTrip(t *testing.T) {
	mr, err := miniredis.Run()
	assert.NilError(t, err)
	defer mr.Close()

	pool := &redis.Pool{
		MaxIdle:      3,
		IdleTimeout:  time.Minute,
		Dial:         func() (redis.Conn, error) { return redis.Dial("tcp", mr.Addr()) },
		TestOnBorrow: redisTestFunc,
	}

	ctx := context.Background()
	store := &redisStore{pool}
	defer store.Close()

	key, err := store.setSecret(ctx, &secretWithTTL{
		Secret: "wibble",
		TTL:    time.Hour,
	})
	assert.NilError(t, err)

	secret, err := store.getSecret(ctx, key)
	assert.NilError(t, err)
	assert.Equal(t, "wibble", secret)

	secret, err = store.getSecret(ctx, key)
	assert.NilError(t, err)
	assert.Equal(t, "", secret)
}
