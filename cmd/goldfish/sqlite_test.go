package main

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"gotest.tools/v3/assert"
	clocks "k8s.io/utils/clock/testing"
)

func testDB() (*sql.DB, error) {
	dsn := "file:test.db?mode=memory"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(createSchemaSQL)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func TestSqliteRoundTrip(t *testing.T) {
	db, err := testDB()
	assert.NilError(t, err)

	now := time.Now()
	clock := clocks.NewFakeClock(now)

	ctx := context.Background()
	store := sqliteStore{db: db, clock: clock}
	defer store.Close()

	key, err := store.setSecret(ctx, &secretWithTTL{
		Secret: "wibble",
		TTL:    time.Hour,
	})
	assert.NilError(t, err)

	secret, err := store.getSecret(ctx, key)
	assert.NilError(t, err)
	assert.Equal(t, "wibble", secret)

	secret, err = store.getSecret(ctx, key)
	assert.NilError(t, err)
	assert.Equal(t, "", secret)
}

func TestSqliteGetSecret_Expired(t *testing.T) {
	db, err := testDB()
	assert.NilError(t, err)

	now := time.Now()
	clock := clocks.NewFakeClock(now)

	ctx := context.Background()
	store := sqliteStore{db: db, clock: clock}
	defer store.Close()

	key, err := store.setSecret(ctx, &secretWithTTL{
		Secret: "wibble",
		TTL:    time.Hour,
	})
	assert.NilError(t, err)

	clock.SetTime(now.Add(2 * time.Hour))

	secret, err := store.getSecret(ctx, key)
	assert.NilError(t, err)
	assert.Equal(t, "", secret)
}

func TestSqliteGetSecret_Expired_Cleanup(t *testing.T) {
	db, err := testDB()
	assert.NilError(t, err)

	now := time.Now()
	clock := clocks.NewFakeClock(now)

	ctx := context.Background()
	store := sqliteStore{db: db, clock: clock}
	defer store.Close()

	key, err := store.setSecret(ctx, &secretWithTTL{
		Secret: "wibble",
		TTL:    time.Hour,
	})
	assert.NilError(t, err)

	store.expireSecrets(ctx, now.Add(2*time.Hour))

	secret, err := store.getSecret(ctx, key)
	assert.NilError(t, err)
	assert.Equal(t, "", secret)
}
