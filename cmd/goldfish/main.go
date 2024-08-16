package main

import (
	"context"
	"errors"
	"fmt"
	log "log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"
)

var (
	listenAddr   string
	pidFilePath  string
	breakerRatio float64

	tlsCertFile string
	tlsKeyFile  string

	limitCount   uint64
	limitPeriod  time.Duration
	limitHeaders string

	storeType        string
	storeSqliteFile  string
	storeSqliteClean time.Duration
	storeRedisAddr   string
	storeRedisUser   string
	storeRedisPass   string
	storeRedisDB     int
	storeRedisTLS    string

	showShutdown bool
)

const (
	sqliteStoreType  = "sqlite"
	redisStoreType   = "redis"
	redisTlsOn       = "on"
	redisTlsOff      = "off"
	redisTlsInsecure = "insecure"
)

func main() {
	pname, err := os.Executable()
	if err != nil {
		log.Error("Unable to determine executable path", "err", err)
		os.Exit(0)
	}
	app := &cli.App{
		Name:            "goldfish",
		Usage:           "Webapp for browser-based one-time secret management",
		ArgsUsage:       " ", // no positional arguments
		Action:          realMain,
		HideHelpCommand: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "addr",
				Usage:       "Server listen address",
				Value:       ":3000",
				Destination: &listenAddr,
				EnvVars:     []string{"LISTEN_ADDR"},
			},
			&cli.StringFlag{
				Name:        "pid-file",
				Usage:       "PID file `path`; empty value to disable file creation",
				Value:       fmt.Sprintf("%s.pid", pname),
				Destination: &pidFilePath,
				EnvVars:     []string{"PID_FILE"},
			},
			&cli.Float64Flag{
				Name:        "breaker-ratio",
				Usage:       "Circuit-breaker failure ratio; zero or less to disable the circuit-breaker",
				Value:       0.1,
				Destination: &breakerRatio,
				EnvVars:     []string{"BREAKER_RATIO"},
			},
			&cli.StringFlag{
				Name:        "backend",
				Usage:       fmt.Sprintf("Backend to use for secret `storage`, either %q or %q", sqliteStoreType, redisStoreType),
				Value:       sqliteStoreType,
				Destination: &storeType,
				EnvVars:     []string{"BACKEND_STORE"},
			},
			&cli.StringFlag{
				Name:        "sqlite-file",
				Usage:       "Database file `path`",
				Value:       fmt.Sprintf("%s.db", pname),
				Category:    "SQLite backend",
				Destination: &storeSqliteFile,
				EnvVars:     []string{"SQLITE_FILE"},
			},
			&cli.DurationFlag{
				Name:        "sqlite-clean",
				Usage:       "Interval for removal of unaccessed expired secrets",
				Value:       time.Hour,
				Category:    "SQLite backend",
				Destination: &storeSqliteClean,
				EnvVars:     []string{"SQLITE_CLEAN"},
			},
			&cli.StringFlag{
				Name:        "redis-addr",
				Usage:       "Redis address",
				Value:       "localhost:6379",
				Category:    "Redis backend",
				Destination: &storeRedisAddr,
				EnvVars:     []string{"REDIS_ADDR"},
			},
			&cli.StringFlag{
				Name:        "redis-user",
				Usage:       "Redis username, if required",
				Category:    "Redis backend",
				Destination: &storeRedisUser,
				EnvVars:     []string{"REDIS_USER"},
			},
			&cli.StringFlag{
				Name:        "redis-pass",
				Usage:       "Redis password, if required",
				Category:    "Redis backend",
				Destination: &storeRedisPass,
				EnvVars:     []string{"REDIS_PASS"},
			},
			&cli.IntFlag{
				Name:        "redis-db",
				Usage:       "Redis db `number`, if required",
				Category:    "Redis backend",
				Destination: &storeRedisDB,
				EnvVars:     []string{"REDIS_DB"},
			},
			&cli.StringFlag{
				Name:        "redis-tls",
				Usage:       fmt.Sprintf("Either %q, %q, or %q", redisTlsOff, redisTlsOn, redisTlsInsecure),
				Value:       redisTlsOff,
				Category:    "Redis backend",
				Destination: &storeRedisTLS,
				EnvVars:     []string{"REDIS_TLS"},
			},
			&cli.StringFlag{
				Name:        "tls-cert",
				Usage:       "Server TLS certificate `file` path",
				Category:    "HTTPS listener",
				Destination: &tlsCertFile,
				EnvVars:     []string{"TLS_CERT_FILE"},
			},
			&cli.StringFlag{
				Name:        "tls-key",
				Usage:       "Server TLS private key `file` path",
				Category:    "HTTPS listener",
				Destination: &tlsKeyFile,
				EnvVars:     []string{"TLS_KEY_FILE"},
			},
			&cli.Uint64Flag{
				Name:        "limit-count",
				Usage:       "Maximum `number` of requests, per IP; zero to disable the limiter",
				Value:       1000,
				Category:    "Rate-limiter",
				Destination: &limitCount,
				EnvVars:     []string{"RATE_LIMIT_COUNT"},
			},
			&cli.DurationFlag{
				Name:        "limit-period",
				Usage:       "Window of `time` for requests, per IP",
				Value:       time.Hour,
				Category:    "Rate-limiter",
				Destination: &limitPeriod,
				EnvVars:     []string{"RATE_LIMIT_PERIOD"},
			},
			&cli.StringFlag{
				Name:        "limit-headers",
				Usage:       "Comma-separated `list` of http request headers that can provide an IP address",
				Value:       "Cf-Connecting-Ip,X-Forwarded-For",
				Category:    "Rate-limiter",
				Destination: &limitHeaders,
				EnvVars:     []string{"RATE_LIMIT_HEADERS"},
			},
		},
	}
	if err = app.Run(os.Args); err != nil {
		log.Error("Failed", "err", err)
		os.Exit(1)
	}
	if showShutdown {
		log.Info("Shutdown")
	}
}

func realMain(*cli.Context) error {
	showShutdown = true

	if err := writePidFile(); err != nil {
		return err
	}
	defer removePidFile()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	secrets, err := newSecretStore(ctx)
	if err != nil {
		return err
	}
	defer secrets.Close()

	limits, err := newLimiterStore()
	if err != nil {
		return err
	}
	defer limits.Close(context.Background())

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           newHandler(secrets, limits),
		ReadHeaderTimeout: time.Minute, // CWE-400 (slowloris) use nginx timeout
	}

	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()

	ll := log.With("addr", listenAddr, "backend", storeType)
	if tlsCertFile != "" && tlsKeyFile != "" {
		ll.Info("Starting HTTPS listener")
		err = server.ListenAndServeTLS(tlsCertFile, tlsKeyFile)
	} else {
		ll.Info("Starting HTTP listener")
		err = server.ListenAndServe()
	}
	if err != nil && errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func writePidFile() error {
	if pidFilePath == "" {
		return nil
	}
	log.Info("Creating pid file", "path", pidFilePath)

	fp, err := os.Create(pidFilePath)
	if err != nil {
		return err
	}
	defer fp.Close()

	pid := os.Getpid()
	_, err = fmt.Fprint(fp, strconv.Itoa(pid))
	return err
}

func removePidFile() {
	if pidFilePath != "" {
		_ = os.Remove(pidFilePath)
	}
}
