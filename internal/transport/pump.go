package transport

import (
	"context"
	"errors"
	"sync"

	"github.com/coder/websocket"
	"github.com/sit/sit/internal/protocol"
)

// ErrConnClosed is returned by Send when the connection is no longer usable.
var ErrConnClosed = errors.New("transport: connection closed")

const sendBuffer = 64

// wsConn wraps a *websocket.Conn with decoupled read/write pumps, exposing the
// Conn interface. A single write pump serializes all writes (websocket forbids
// concurrent writers); a single read pump decodes inbound frames.
type wsConn struct {
	ws   *websocket.Conn
	info SessionInfo

	sendCh chan protocol.Envelope
	recvCh chan protocol.Envelope
	done   chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// newWSConn starts the pumps for an already-established websocket connection.
// It sets the read limit to MaxFrameBytes to bound memory on hostile peers.
func newWSConn(ws *websocket.Conn, info SessionInfo) *wsConn {
	ws.SetReadLimit(protocol.MaxFrameBytes)
	c := &wsConn{
		ws:     ws,
		info:   info,
		sendCh: make(chan protocol.Envelope, sendBuffer),
		recvCh: make(chan protocol.Envelope),
		done:   make(chan struct{}),
	}
	go c.writePump()
	go c.readPump()
	return c
}

func (c *wsConn) readPump() {
	defer close(c.recvCh)
	ctx := context.Background()
	for {
		typ, data, err := c.ws.Read(ctx)
		if err != nil {
			c.Close("read error: " + err.Error())
			return
		}
		if typ != websocket.MessageText {
			continue // protocol uses text frames only
		}
		env, err := protocol.Decode(data)
		if err != nil {
			// Malformed/oversize frame: drop it but keep the connection.
			continue
		}
		select {
		case c.recvCh <- env:
		case <-c.done:
			return
		}
	}
}

func (c *wsConn) writePump() {
	ctx := context.Background()
	for {
		select {
		case <-c.done:
			return
		case env := <-c.sendCh:
			b, err := protocol.Encode(env)
			if err != nil {
				continue // skip un-encodable / oversize frame
			}
			if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
				c.Close("write error: " + err.Error())
				return
			}
		}
	}
}

func (c *wsConn) Send(ctx context.Context, e protocol.Envelope) error {
	select {
	case <-c.done:
		return ErrConnClosed
	default:
	}
	select {
	case c.sendCh <- e:
		return nil
	case <-c.done:
		return ErrConnClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *wsConn) Recv() <-chan protocol.Envelope { return c.recvCh }

func (c *wsConn) Info() SessionInfo { return c.info }

func (c *wsConn) Done() <-chan struct{} { return c.done }

func (c *wsConn) Close(reason string) error {
	c.closeOnce.Do(func() {
		close(c.done)
		c.closeErr = c.ws.Close(websocket.StatusNormalClosure, truncateReason(reason))
	})
	return c.closeErr
}

// truncateReason keeps the WS close reason within the 123-byte protocol limit.
func truncateReason(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
