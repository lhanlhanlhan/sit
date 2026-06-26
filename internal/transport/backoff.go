package transport

import (
	"math/rand"
	"time"
)

// Backoff computes exponential reconnect delays with jitter.
// delay(n) = min(base * factor^n, cap) ± jitterFrac.
// Per ADR-005: base=1s, factor=2, cap=60s, jitter ±20%.
type Backoff struct {
	Base       time.Duration
	Factor     float64
	Cap        time.Duration
	JitterFrac float64 // e.g. 0.2 for ±20%

	attempt int
	rand    *rand.Rand
}

// NewBackoff returns a Backoff with the ADR-005 defaults.
func NewBackoff() *Backoff {
	return &Backoff{
		Base:       1 * time.Second,
		Factor:     2,
		Cap:        60 * time.Second,
		JitterFrac: 0.2,
		rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Reset clears the attempt counter (call after a successful connection).
func (b *Backoff) Reset() { b.attempt = 0 }

// base returns the un-jittered, capped delay for the current attempt.
func (b *Backoff) base() time.Duration {
	d := float64(b.Base)
	for i := 0; i < b.attempt; i++ {
		d *= b.Factor
		if d >= float64(b.Cap) {
			return b.Cap
		}
	}
	if d > float64(b.Cap) {
		return b.Cap
	}
	return time.Duration(d)
}

// Next returns the next delay (with jitter applied) and advances the attempt.
func (b *Backoff) Next() time.Duration {
	base := b.base()
	b.attempt++
	if b.JitterFrac <= 0 {
		return base
	}
	if b.rand == nil {
		b.rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	// jitter in [-JitterFrac, +JitterFrac]
	delta := (b.rand.Float64()*2 - 1) * b.JitterFrac
	return time.Duration(float64(base) * (1 + delta))
}
