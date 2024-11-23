package main

import (
	"crypto/rand"
	"fmt"
	log "log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/sethvargo/go-limiter"
	"github.com/streadway/handy/breaker"

	"github.com/digitalocean-labs/goldfish/app"
)

func newHandler(secrets secretStore, limits limiter.Store) http.Handler {
	mux := http.NewServeMux()
	rate := newRateLimiter(limits)
	mux.Handle("/{$}", http.RedirectHandler("/app/", http.StatusFound))
	mux.Handle("/app/", staticCacheControl(http.StripPrefix("/app", http.FileServer(app.FS))))
	mux.Handle("GET /secret", rate.Handle(dynamicCacheControl(getSecret(secrets))))
	mux.Handle("POST /secret", rate.Handle(dynamicCacheControl(setSecret(secrets))))
	return circuitBreaker(panicRecovery(mux))
}

func staticCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ref: https://web.dev/articles/http-cache
		if app.Embedded {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func dynamicCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ref: https://web.dev/articles/http-cache
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func circuitBreaker(handler http.Handler) http.Handler {
	if breakerRatio > 0 {
		cb := breaker.NewBreaker(breakerRatio)
		return breaker.Handler(cb, breaker.DefaultStatusCodeValidator, handler)
	}
	return handler
}

func panicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				stack := string(debug.Stack())
				err := fmt.Errorf("panic: %v; stack: %s", p, stack)
				internalError(w, "request failed", err)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func getSecret(store secretStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, err := parseGetRequest(r)
		if key == "" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		secret, err := store.getSecret(r.Context(), key)
		if err != nil {
			internalError(w, "unexpected error", err)
			return
		}
		if secret == "" {
			http.Error(w, "key not found or expired", http.StatusNotFound)
			return
		}
		writeSuccess(w, secret)
	}
}

func setSecret(store secretStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret, err := parseSetRequest(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		key, err := store.setSecret(r.Context(), secret)
		if err != nil {
			internalError(w, "unexpected error", err)
			return
		}
		writeSuccess(w, key)
	}
}

func parseGetRequest(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	if len(key) > 255 {
		return "", fmt.Errorf("key is too long")
	}
	if !validSecretKey.MatchString(key) {
		return "", fmt.Errorf("key is invalid")
	}
	return key, nil
}

func parseSetRequest(r *http.Request) (*secretWithTTL, error) {
	secret := strings.TrimSpace(r.PostFormValue("secret"))
	if secret == "" {
		return nil, fmt.Errorf("secret is required")
	}
	if len(secret) > 4096 {
		return nil, fmt.Errorf("secret is too long")
	}
	ttlTxt := strings.TrimSpace(r.PostFormValue("ttl"))
	if ttlTxt == "" {
		return nil, fmt.Errorf("ttl is required")
	}
	ttlHours, err := strconv.Atoi(ttlTxt)
	if err != nil {
		return nil, fmt.Errorf("ttl is invalid")
	}
	if ttlHours < 1 || ttlHours > 72 {
		return nil, fmt.Errorf("ttl is too long")
	}
	return &secretWithTTL{
		Secret: secret,
		TTL:    time.Duration(ttlHours) * time.Hour,
	}, nil
}

func writeSuccess(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, msg)
}

func internalError(w http.ResponseWriter, msg string, err error) {
	errorID := newErrorID()
	log.Error(msg, "err_id", errorID, "err", err)
	http.Error(w, fmt.Sprintf("ID: %s; %s", errorID, msg), http.StatusInternalServerError)
}

func newErrorID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%x", buf)
}
