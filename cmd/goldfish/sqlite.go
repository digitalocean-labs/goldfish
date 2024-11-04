package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	log "log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const createSchemaSQL = `
create table if not exists secrets (
    secret_key    text      primary key,
    secret_value  text      not null,
    expire_at     timestamp not null
);
create index if not exists expireAtIdx on secrets (expire_at);
`

const (
	setSecretSQL = `INSERT INTO secrets (secret_key, secret_value, expire_at) VALUES (?, ?, ?)`
	getSecretSQL = `SELECT secret_value FROM secrets WHERE secret_key = ? AND expire_at > ?`
	deleteKeySQL = `DELETE FROM secrets WHERE secret_key = ?`
	expireSQL    = `DELETE FROM secrets WHERE expire_at < ?`
)

// for replacement in tests
type timeNowFunc func() time.Time

type sqliteStore struct {
	db  *sql.DB
	now timeNowFunc
}

func newSqliteStore(ctx context.Context) (secretStore, error) {
	dsn := fmt.Sprintf("file:%s", storeSqliteFile)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	_, err = db.ExecContext(ctx, createSchemaSQL)
	if err != nil {
		db.Close()
		return nil, err
	}
	store := &sqliteStore{
		db:  db,
		now: time.Now,
	}
	go regularDatabaseCleanup(ctx, db)
	return store, nil
}

func (s *sqliteStore) Close() error {
	return s.db.Close()
}

func (s *sqliteStore) setSecret(ctx context.Context, req *secretWithTTL) (string, error) {
	key := newSecretKey()
	expireAt := s.now().Add(req.TTL)
	_, err := s.db.ExecContext(ctx, setSecretSQL, key, req.Secret, expireAt)
	return key, err
}

func (s *sqliteStore) getSecret(ctx context.Context, key string) (string, error) {
	var secret string
	err := s.db.QueryRowContext(ctx, getSecretSQL, key, s.now()).Scan(&secret)
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
