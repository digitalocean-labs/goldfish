package main

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestNewSecretsKey(t *testing.T) {
	for i := 0; i < 100; i++ {
		key := newSecretKey()
		assert.Assert(t, validSecretKey.MatchString(key), key)
	}
}
