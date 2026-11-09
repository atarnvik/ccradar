package main

import (
	"os"
	"testing"
)

// Tests never read or write the real cache dir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ccradar-test-cache")
	if err != nil {
		panic(err)
	}
	os.Setenv("CCRADAR_CACHE_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
