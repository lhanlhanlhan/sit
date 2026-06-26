package node

import (
	"context"
	"time"

	"github.com/sit/sit/internal/protocol"
	"github.com/sit/sit/internal/transport"
)

// Client drives one connected session: sends register, runs the heartbeat
// ticker, and processes inbound instructions (ack → execute → result).
// Reconnect/backoff is handled by Run, which re-establishes a Client per session.
type Client struct {
	conn      transport.Conn
	reporter  *Reporter
	executor  *Executor
	heartbeat time.Duration
}

// NewClient builds a Client over an established connection.
func NewClient(conn transport.Conn, reporter *Reporter, executor *Executor, heartbeat time.Duration) *Client {
	if heartbeat <= 0 {
		heartbeat = defaultHeartbeatSec * time.Second
	}
	return &Client{conn: conn, reporter: reporter, executor: executor, heartbeat: heartbeat}
}

// Serve runs the session until the connection closes or ctx is cancelled.
// On entry it sends register + an "online" event, then loops handling frames
// and emitting heartbeats. Returns when the connection terminates.
func (c *Client) Serve(ctx context.Context) error {
	// 1. Announce presence.
	if err := c.send(ctx, mustEnvelope(protocol.NewNotification(c.reporter.Register()))); err != nil {
		return err
	}
	_ = c.send(ctx, mustEnvelope(protocol.NewNotification(c.reporter.Event("online", ""))))

	// 2. Heartbeat ticker.
	ticker := time.NewTicker(c.heartbeat)
	defer ticker.Stop()

	recv := c.conn.Recv()
	for {
		select {
		case <-ctx.Done():
			_ = c.conn.Close("context cancelled")
			return ctx.Err()
		case <-c.conn.Done():
			return nil
		case <-ticker.C:
			_ = c.send(ctx, mustEnvelope(protocol.NewNotification(c.reporter.Heartbeat())))
		case env, ok := <-recv:
			if !ok {
				return nil // connection closed
			}
			c.handleFrame(ctx, env)
		}
	}
}

// handleFrame dispatches one inbound envelope. Instructions are acked
// immediately, then executed; the result is reported back.
func (c *Client) handleFrame(ctx context.Context, env protocol.Envelope) {
	if env.Type != protocol.TypeInstruction {
		return // node only acts on instructions; acks/others ignored
	}
	instr, err := env.AsInstruction()
	if err != nil {
		return
	}
	// Ack receipt immediately (idempotent dispatch on the manager).
	_ = c.send(ctx, mustEnvelope(protocol.NewAck(env.ID)))

	// Execute (dedup keyed by instruction id) and report the result.
	res := c.executor.Execute(ctx, env.ID, instr)
	resEnv, err := protocol.NewNotification(res)
	if err != nil {
		return
	}
	_ = c.send(ctx, resEnv)
}

func (c *Client) send(ctx context.Context, env protocol.Envelope) error {
	return c.conn.Send(ctx, env)
}

// mustEnvelope panics only on programmer error (marshal of a static struct).
func mustEnvelope(env protocol.Envelope, err error) protocol.Envelope {
	if err != nil {
		panic(err)
	}
	return env
}
