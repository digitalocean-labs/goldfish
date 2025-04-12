package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"time"

	pwd "github.com/sethvargo/go-password/password"
)

type secretWithTTL struct {
	Secret string
	TTL    time.Duration
}

type secretStore interface {
	setSecret(ctx context.Context, secret *secretWithTTL) (key string, err error)
	getSecret(ctx context.Context, key string) (secret string, err error)
	io.Closer
}

// allow older hex keys and new alphanumeric ones
var validSecretKey = regexp.MustCompile(`^[[:alnum:]]{32}$`)

func newSecretKey() (string, error) {
	return pwd.Generate(32, 10, 0, false, true)
}

func newSecretStore(ctx context.Context) (secretStore, error) {
	switch storeType {
	case sqliteStoreType:
		return newSqliteStore(ctx)
	case redisStoreType:
		return newRedisStore(), nil
	default:
		return nil, fmt.Errorf("unknown backend storage %q", storeType)
	}
}
