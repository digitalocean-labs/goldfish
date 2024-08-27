package main

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/gomodule/redigo/redis"
	"github.com/sethvargo/go-limiter"
	"github.com/sethvargo/go-limiter/httplimit"
	"github.com/sethvargo/go-limiter/memorystore"
	"github.com/sethvargo/go-limiter/noopstore"
	"github.com/sethvargo/go-redisstore"
)

func newRateLimiter(store limiter.Store) *httplimit.Middleware {
	mw, err := httplimit.NewMiddleware(store, newLimiterKeyFunc())
	if err != nil {
		// store and key function are never nil here
		panic(err)
	}
	return mw
}

func newLimiterKeyFunc() httplimit.KeyFunc {
	var headers []string
	if len(limitHeaders) > 0 {
		headers = strings.Split(limitHeaders, ",")
	}
	keyFunc := httplimit.IPKeyFunc(headers...)
	if storeType != redisStoreType {
		return keyFunc
	}
	return func(r *http.Request) (string, error) {
		key, err := keyFunc(r)
		if err != nil {
			return "", err
		}
		data := sha256.Sum256([]byte(key))
		return redisKey(fmt.Sprintf("%x", data)), nil
	}
}

func newLimiterStore() (limiter.Store, error) {
	if limitCount == 0 {
		return noopstore.New()
	}
	if storeType != redisStoreType {
		return memorystore.New(&memorystore.Config{
			Tokens:   limitCount,
			Interval: limitPeriod,
		})
	}
	return redisstore.New(&redisstore.Config{
		Tokens:   limitCount,
		Interval: limitPeriod,
		Dial: func() (redis.Conn, error) {
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
		},
	})
}
