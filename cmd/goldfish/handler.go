package main

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	log "log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/csrf"
	"github.com/sethvargo/go-limiter"
	"github.com/streadway/handy/breaker"

	"github.com/digitalocean-labs/goldfish/app"
)

func newHandler(secrets secretStore, limits limiter.Store) http.Handler {
	mux := http.NewServeMux()
	rate := newRateLimiter(limits)
	mux.Handle("/{$}", http.RedirectHandler("/app/", http.StatusFound))
	mux.Handle("/app/", staticCacheControl(http.StripPrefix("/app", http.FileServer(app.FS))))
	mux.Handle("GET /token", rate.Handle(dynamicCacheControl(http.HandlerFunc(csrfToken))))
	mux.Handle("POST /push", rate.Handle(dynamicCacheControl(setSecret(secrets))))
	mux.Handle("POST /pull", rate.Handle(dynamicCacheControl(getSecret(secrets))))
	return circuitBreaker(panicRecovery(csrfMiddleware(mux)))
}

func staticCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		// Ref: https://web.dev/articles/http-cache
		if app.Embedded {
			headers.Set("Cache-Control", "no-cache")
		} else {
			headers.Set("Cache-Control", "no-store")
		}
		setSecurityHeaders(headers)
		next.ServeHTTP(w, r)
	})
}

func dynamicCacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		// Ref: https://web.dev/articles/http-cache
		headers.Set("Cache-Control", "no-store")
		setSecurityHeaders(headers)
		next.ServeHTTP(w, r)
	})
}

// Ref: https://blog.appcanary.com/2017/http-security-headers.html
func setSecurityHeaders(headers http.Header) {
	headers.Set("X-XSS-Protection", "1; mode=block")
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
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
				internalError(w, err)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func csrfMiddleware(next http.Handler) http.Handler {
	if csrfKey == csrfOff {
		log.Warn("CSRF protection is disabled")
		return next
	}
	var key []byte
	if csrfKey != "" {
		sum := sha256.Sum256([]byte(csrfKey))
		key = sum[:]
	} else {
		key = make([]byte, 32)
		_, _ = rand.Read(key)
	}
	trustedOrigins := csrfOrigins.Value()
	log.Info("CSRF trusted origins", "value", trustedOrigins)
	mw := csrf.Protect(key, csrf.Secure(csrfSecure), csrf.CookieName("_goldfish"), csrf.TrustedOrigins(trustedOrigins))
	return mw(next)
}

func csrfToken(w http.ResponseWriter, r *http.Request) {
	if csrfKey == csrfOff {
		writeSuccess(w, "csrf-off")
	} else {
		writeSuccess(w, csrf.Token(r))
	}
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
			internalError(w, err)
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
			internalError(w, err)
			return
		}
		writeSuccess(w, key)
	}
}

func parseGetRequest(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.PostFormValue("key"))
	if key == "" {
		return "", errors.New("key is required")
	}
	if !validSecretKey.MatchString(key) {
		return "", errors.New("key is invalid")
	}
	return key, nil
}

func parseSetRequest(r *http.Request) (*secretWithTTL, error) {
	secret := strings.TrimSpace(r.PostFormValue("secret"))
	if secret == "" {
		return nil, errors.New("secret is required")
	}
	if len(secret) > 4096 {
		return nil, errors.New("secret is too long")
	}
	ttlTxt := strings.TrimSpace(r.PostFormValue("ttl"))
	if ttlTxt == "" {
		return nil, errors.New("ttl is required")
	}
	ttlHours, err := strconv.Atoi(ttlTxt)
	if err != nil {
		return nil, errors.New("ttl is invalid")
	}
	if ttlHours < 1 || ttlHours > 72 {
		return nil, errors.New("ttl is too long")
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

func internalError(w http.ResponseWriter, err error) {
	errorID := newErrorID()
	log.Error("request failed", "err_id", errorID, "err", err)
	http.Error(w, fmt.Sprintf("Error ID: %s", errorID), http.StatusInternalServerError)
}

func newErrorID() string {
	buf := make([]byte, 4)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%x", buf)
}
