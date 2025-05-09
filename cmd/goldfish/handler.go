package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	log "log/slog"
	"net/http"
	"net/url"
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

var csrfSafeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

var csrfSafeFetches = map[string]bool{
	"same-origin": true,
	"none":        true,
}

// adapted from https://github.com/golang/go/issues/73626
func csrfCheck(r *http.Request) error {
	if csrfSafeMethods[r.Method] {
		return nil
	}
	secFetchSite := r.Header.Get("Sec-Fetch-Site")
	if csrfSafeFetches[secFetchSite] {
		return nil
	}
	origin := r.Header.Get("Origin")
	if secFetchSite == "" {
		if origin == "" {
			return errors.New("not a browser request")
		}
		parsed, err := url.Parse(origin)
		if err != nil {
			return fmt.Errorf("url.Parse: %w", err)
		}
		if parsed.Host == r.Host {
			return nil
		}
	}
	return fmt.Errorf("Sec-Fetch-Site %q, Origin %q, Host %q", secFetchSite, origin, r.Host)
}

func csrfMiddleware(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := csrfCheck(r); err != nil {
			errorID := newErrorID()
			log.Error("csrf check failed", "err_id", errorID, "err", err)
			http.Error(w, fmt.Sprintf("Error ID: %s", errorID), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
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
