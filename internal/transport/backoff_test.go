package transport

import (
	"testing"
	"time"
)

func TestBackoff_ExponentialSequenceCapped(t *testing.T) {
	b := &Backoff{Base: time.Second, Factor: 2, Cap: 60 * time.Second, JitterFrac: 0}
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second, // 64 capped to 60
		60 * time.Second,
	}
	for i, w := range want {
		got := b.Next()
		if got != w {
			t.Errorf("attempt %d: got %v want %v", i, got, w)
		}
	}
}

func TestBackoff_JitterWithinBounds(t *testing.T) {
	b := NewBackoff() // base 1s, factor 2, cap 60s, jitter 0.2
	for i := 0; i < 200; i++ {
		bb := &Backoff{Base: time.Second, Factor: 2, Cap: 60 * time.Second, JitterFrac: 0.2}
		bb.attempt = i % 7
		base := bb.base()
		got := bb.Next()
		lo := time.Duration(float64(base) * 0.8)
		hi := time.Duration(float64(base) * 1.2)
		if got < lo || got > hi {
			t.Fatalf("attempt %d: %v outside [%v,%v]", bb.attempt, got, lo, hi)
		}
	}
	_ = b
}

func TestBackoff_ResetRestartsSequence(t *testing.T) {
	b := &Backoff{Base: time.Second, Factor: 2, Cap: 60 * time.Second, JitterFrac: 0}
	b.Next()
	b.Next()
	b.Reset()
	if got := b.Next(); got != time.Second {
		t.Errorf("after reset: got %v want 1s", got)
	}
}
