package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"regexp"
	"time"
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

var validSecretKey = regexp.MustCompile(`^[[:xdigit:]]{32}$`)

func newSecretKey() (string, error) {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", buf), nil
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
