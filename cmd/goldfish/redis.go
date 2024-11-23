package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	log "log/slog"
	"time"

	"github.com/gomodule/redigo/redis"
)

type redisStore struct {
	db *redis.Pool
}

func newRedisStore() secretStore {
	log.Info("Using Redis secret store", "addr", storeRedisAddr, "tls", storeRedisTLS)
	pool := &redis.Pool{
		MaxIdle:      3,
		IdleTimeout:  2 * time.Minute,
		Dial:         redisDialFunc,
		TestOnBorrow: redisTestFunc,
	}
	return &redisStore{pool}
}

func (r *redisStore) Close() error {
	return r.db.Close()
}

func (r *redisStore) setSecret(ctx context.Context, req *secretWithTTL) (string, error) {
	conn := r.db.Get()
	defer conn.Close()

	secretKey, err := newSecretKey()
	if err != nil {
		return "", err
	}
	ttl := int64(req.TTL.Seconds())
	key := redisKey("s", secretKey)
	_, err = redis.DoContext(conn, ctx, "SET", key, req.Secret, "EX", ttl)
	if err != nil {
		return "", err
	}
	return secretKey, nil
}

func (r *redisStore) getSecret(ctx context.Context, secretKey string) (string, error) {
	conn := r.db.Get()
	defer conn.Close()

	key := redisKey("s", secretKey)
	secret, err := redis.String(redis.DoContext(conn, ctx, "GETDEL", key))
	if err != nil {
		if errors.Is(err, redis.ErrNil) {
			return "", nil
		}
		return "", err
	}
	return secret, nil
}

func redisDialFunc() (redis.Conn, error) {
	var opts []redis.DialOption
	if storeRedisUser != "" {
		opts = append(opts, redis.DialUsername(storeRedisUser))
	}
	if storeRedisPass != "" {
		opts = append(opts, redis.DialPassword(storeRedisPass))
	}
	if storeRedisDB > 0 {
		opts = append(opts, redis.DialDatabase(storeRedisDB))
	}
	if tlsCfg := redisTLS(); tlsCfg != nil {
		opts = append(opts, redis.DialUseTLS(true), redis.DialTLSConfig(tlsCfg))
	}
	return redis.Dial("tcp", storeRedisAddr, opts...)
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

func redisTestFunc(c redis.Conn, _ time.Time) error {
	_, err := c.Do("PING")
	return err
}

func redisKey(prefix, key string) string {
	if storeRedisNS != "" {
		return fmt.Sprintf("%s:%s:%s", storeRedisNS, prefix, key)
	}
	return fmt.Sprintf("%s:%s", prefix, key)
}
