package main

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
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

var validSecretKey = regexp.MustCompile(`^[[:xdigit:]]+$`)

func newSecretKey() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
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
