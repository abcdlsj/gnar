package pio

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLimitReader(t *testing.T) {
	limiter := NewLimitReader(strings.NewReader("123456"), 2)

	st := time.Now()
	_, err := limiter.Read([]byte("123456"))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Read cost: %v, limit: 2", time.Since(st))
}

func TestLimitWriter(t *testing.T) {
	limiter := NewLimitWriter(os.Stdout, 2)

	st := time.Now()
	_, err := limiter.Write([]byte("123456"))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Write cost: %v, limit: 2", time.Since(st))
}
