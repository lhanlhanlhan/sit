package node

import (
	"context"
	"crypto/tls"
	"net/http"
	"runtime"
	"time"

	"github.com/sit/sit/internal/transport"
)

// Agent is the long-running node process: it dials the Manager, serves a
// session, and reconnects with exponential backoff when the link drops.
// "Disconnect is normal" — the loop never gives up while ctx is live.
type Agent struct {
	cfg      Config
	identity Identity
	version  string
	executor *Executor
}

// NewAgent assembles an Agent from loaded config + identity.
func NewAgent(cfg Config, id Identity, version string, predefined map[string]PredefinedFunc) *Agent {
	return &Agent{cfg: cfg, identity: id, version: version, executor: NewExecutor(predefined)}
}

// Run connects and serves sessions until ctx is cancelled. Each dropped session
// triggers a backoff-delayed reconnect (and a fresh register on reconnect).
func (a *Agent) Run(ctx context.Context) error {
	bo := transport.NewBackoff()
	hb := time.Duration(a.cfg.HeartbeatSec) * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := a.dial(ctx)
		if err != nil {
			if !sleepCtx(ctx, bo.Next()) {
				return ctx.Err()
			}
			continue
		}
		bo.Reset()
		reporter := NewReporter(a.identity.NodeID, a.version)
		client := NewClient(conn, reporter, a.executor, hb)
		_ = client.Serve(ctx) // returns on disconnect or ctx cancel

		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Disconnected: brief backoff before re-dialing.
		if !sleepCtx(ctx, bo.Next()) {
			return ctx.Err()
		}
	}
}

// dial opens one outbound connection, presenting the long-term credential as
// "<node_id>:<secret>" in the bearer token (handshake binds identity server-side).
func (a *Agent) dial(ctx context.Context) (transport.Conn, error) {
	opts := transport.DialOptions{
		Endpoints:   a.cfg.Endpoints,
		AuthToken:   a.identity.NodeID + ":" + a.identity.Secret,
		DialTimeout: 10 * time.Second,
	}
	if a.cfg.InsecureSkipVerify {
		opts.HTTPClient = insecureClient(10 * time.Second)
	}
	return transport.Dial(ctx, opts)
}

// insecureClient disables TLS verification — TEST/dev only, never production.
func insecureClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // dev-only flag
		},
	}
}

// sleepCtx waits for d or ctx cancellation; returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// DefaultStateDir returns the platform default node state directory.
func DefaultStateDir() string {
	if runtime.GOOS == "darwin" {
		return "/usr/local/var/lib/sit"
	}
	return "/var/lib/sit"
}
