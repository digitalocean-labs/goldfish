package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	log "log/slog"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const createSchemaSQL = `
create table secrets (
    secret_key    text      primary key,
    secret_value  text      not null,
    created_at    timestamp not null,
    expire_at     timestamp not null
);
create index expireAtIdx on secrets (expire_at);
`

const (
	setSecretSQL = `INSERT INTO secrets (secret_key, secret_value, created_at, expire_at) VALUES (?, ?, ?, ?)`
	getSecretSQL = `SELECT secret_value FROM secrets WHERE secret_key = ? AND expire_at > ?`
	deleteKeySQL = `DELETE FROM secrets WHERE secret_key = ?`
	expireSQL    = `DELETE FROM secrets WHERE expire_at < ?`
)

type sqliteStore struct {
	db *sql.DB
}

func newSqliteStore(ctx context.Context) (secretStore, error) {
	var dbMissing bool
	if _, err := os.Stat(storeSqliteFile); err != nil {
		dbMissing = os.IsNotExist(err)
	}

	dsn := fmt.Sprintf("file:%s", storeSqliteFile)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	if dbMissing {
		log.Info("Creating database", "path", storeSqliteFile)
		if err = createDatabase(ctx, db); err != nil {
			db.Close()
			return nil, err
		}
	}

	go regularDatabaseCleanup(ctx, db)

	return &sqliteStore{db}, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

func createDatabase(ctx context.Context, db *sql.DB) error {
	for _, stmt := range strings.Split(createSchemaSQL, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		_, err := db.ExecContext(ctx, stmt)
		if err != nil {
			return err
		}
	}
	return nil
}

func regularDatabaseCleanup(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(storeSqliteClean)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			expireSecrets(ctx, db, now)
		}
	}
}

func expireSecrets(ctx context.Context, db *sql.DB, now time.Time) {
	_, err := db.ExecContext(ctx, expireSQL, now)
	if err != nil {
		log.Warn("expire secrets failed", "err", err)
	}
}

func (s *sqliteStore) setSecret(ctx context.Context, req *secretWithTTL) (string, error) {
	key := newSecretKey()
	now := time.Now()
	expireAt := now.Add(req.TTL)
	_, err := s.db.ExecContext(ctx, setSecretSQL, key, req.Secret, now, expireAt)
	return key, err
}

func (s *sqliteStore) getSecret(ctx context.Context, key string) (string, error) {
	var secret string
	err := s.db.QueryRowContext(ctx, getSecretSQL, key, time.Now()).Scan(&secret)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	_, err = s.db.ExecContext(ctx, deleteKeySQL, key)
	if err != nil {
		log.Warn("failed to delete", "err", err)
	}
	return secret, nil
}
