package main

import (
	"context"
	"crypto/tls"
	"errors"
	log "log/slog"

	"github.com/redis/go-redis/v9"
)

type redisStore struct {
	db *redis.Client
}

func newRedisStore() secretStore {
	db := redis.NewClient(&redis.Options{
		Addr:      storeRedisAddr,
		Username:  storeRedisUser,
		Password:  storeRedisPass,
		DB:        storeRedisDB,
		TLSConfig: redisTLS(),
	})
	return &redisStore{db}
}

func redisTLS() *tls.Config {
	switch storeRedisTLS {
	case redisTlsOn:
		return &tls.Config{MinVersion: tls.VersionTLS12}
	case redisTlsInsecure:
		return &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}
	default:
		return nil
	}
}

func (r *redisStore) Close() error {
	return r.db.Close()
}

func (r *redisStore) setSecret(ctx context.Context, req *secretWithTTL) (string, error) {
	secretKey := newSecretKey()
	err := r.db.Set(ctx, redisKey(secretKey), req.Secret, req.TTL).Err()
	return secretKey, err
}

func (r *redisStore) getSecret(ctx context.Context, secretKey string) (string, error) {
	key := redisKey(secretKey)
	secret, err := r.db.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	if err = r.db.Del(ctx, key).Err(); err != nil {
		log.Warn("failed to delete", "err", err)
	}
	return secret, nil
}

func redisKey(key string) string {
	if storeRedisNS != "" {
		return storeRedisNS + key
	}
	return key
}
