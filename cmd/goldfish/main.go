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

	"github.com/tomcz/gotools/errgroup"
	"github.com/tomcz/gotools/quiet"
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
	storeRedisNS     string
	storeRedisTLS    string

	logLevel  string
	logFormat string

	showShutdown bool

	version string
)

const (
	skipPidFile      = "skip"
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
		os.Exit(1)
	}
	app := &cli.App{
		Name:            "goldfish",
		Usage:           "Webapp for browser-based one-time secret management",
		ArgsUsage:       " ", // no positional arguments
		Before:          setupLogging,
		Action:          realMain,
		Version:         version,
		HideHelpCommand: true,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "addr",
				Usage:       "Server listen address",
				Value:       ":3000",
				Category:    "Application",
				Destination: &listenAddr,
				EnvVars:     []string{"LISTEN_ADDR"},
			},
			&cli.StringFlag{
				Name:        "pid-file",
				Usage:       fmt.Sprintf("PID file `path`; use %q to disable file creation", skipPidFile),
				Value:       fmt.Sprintf("%s.pid", pname),
				Category:    "Application",
				Destination: &pidFilePath,
				EnvVars:     []string{"PID_FILE"},
			},
			&cli.Float64Flag{
				Name:        "breaker-ratio",
				Usage:       "Circuit-breaker failure ratio; zero or less to disable the circuit-breaker",
				Value:       0.1,
				Category:    "Application",
				Destination: &breakerRatio,
				EnvVars:     []string{"BREAKER_RATIO"},
			},
			&cli.StringFlag{
				Name:        "backend",
				Usage:       fmt.Sprintf("Backend to use for secret `storage`, either %q or %q", sqliteStoreType, redisStoreType),
				Value:       sqliteStoreType,
				Category:    "Application",
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
				Name:        "redis-ns",
				Usage:       "Redis namespace, if required",
				Category:    "Redis backend",
				Destination: &storeRedisNS,
				EnvVars:     []string{"REDIS_NS"},
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
				Category:    "Rate-limiter",
				Destination: &limitHeaders,
				EnvVars:     []string{"RATE_LIMIT_HEADERS"},
			},
			&cli.StringFlag{
				Name:        "log-level",
				Usage:       "Log `severity` level, one of \"debug\", \"info\", \"warn\", or \"error\"",
				Value:       "info",
				Category:    "Logging",
				Destination: &logLevel,
				EnvVars:     []string{"LOG_LEVEL"},
			},
			&cli.StringFlag{
				Name:        "log-format",
				Usage:       "Structured log format, one of \"plain\", \"text\", or \"json\"",
				Value:       "plain",
				Category:    "Logging",
				Destination: &logFormat,
				EnvVars:     []string{"LOG_FORMAT"},
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
	gracefulTimeout := 100 * time.Millisecond
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
	defer quiet.Close(secrets)

	limits, err := newLimiterStore()
	if err != nil {
		return err
	}
	defer quiet.CloseWithTimeout(limits.Close, gracefulTimeout)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           newHandler(secrets, limits),
		ReadHeaderTimeout: time.Minute, // CWE-400 (slowloris) use nginx timeout
	}

	group, ctx := errgroup.NewContext(ctx)
	group.Go(func() error {
		ll := log.With("addr", listenAddr)
		if tlsCertFile != "" && tlsKeyFile != "" {
			ll.Info("Starting HTTPS listener")
			return server.ListenAndServeTLS(tlsCertFile, tlsKeyFile)
		}
		ll.Info("Starting HTTP listener")
		return server.ListenAndServe()
	})
	group.Go(func() error {
		<-ctx.Done()
		quiet.CloseWithTimeout(server.Shutdown, gracefulTimeout)
		return nil
	})
	err = group.Wait()
	if err != nil && errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func writePidFile() error {
	if pidFilePath == skipPidFile {
		return nil
	}
	log.Info("Creating PID file", "path", pidFilePath)

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
	if pidFilePath != skipPidFile {
		_ = os.Remove(pidFilePath)
	}
}

func setupLogging(*cli.Context) error {
	var level log.Level
	switch logLevel {
	case "debug":
		level = log.LevelDebug
	case "warn":
		level = log.LevelWarn
	case "error":
		level = log.LevelError
	default:
		level = log.LevelInfo
	}
	switch logFormat {
	case "text":
		opts := &log.HandlerOptions{Level: level}
		h := log.NewTextHandler(os.Stderr, opts)
		log.SetDefault(log.New(h))
	case "json":
		opts := &log.HandlerOptions{Level: level}
		h := log.NewJSONHandler(os.Stderr, opts)
		log.SetDefault(log.New(h))
	default:
		log.SetLogLoggerLevel(level)
	}
	return nil
}
