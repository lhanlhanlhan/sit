package node

import (
	"context"
	"sync"

	"github.com/sit/sit/internal/protocol"
	"github.com/sit/sit/internal/transport"
)

// fakeConn is an in-memory transport.Conn for node client tests.
type fakeConn struct {
	mu     sync.Mutex
	sent   []protocol.Envelope
	recvCh chan protocol.Envelope
	doneCh chan struct{}
	closed bool
}

func newFakeConn() *fakeConn {
	return &fakeConn{
		recvCh: make(chan protocol.Envelope, 16),
		doneCh: make(chan struct{}),
	}
}

func (f *fakeConn) Send(ctx context.Context, e protocol.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return transport.ErrConnClosed
	}
	f.sent = append(f.sent, e)
	return nil
}

func (f *fakeConn) Recv() <-chan protocol.Envelope { return f.recvCh }

func (f *fakeConn) Close(reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.doneCh)
	return nil
}

func (f *fakeConn) Info() transport.SessionInfo { return transport.SessionInfo{} }
func (f *fakeConn) Done() <-chan struct{}       { return f.doneCh }

// deliver pushes an inbound envelope to the client.
func (f *fakeConn) deliver(e protocol.Envelope) { f.recvCh <- e }

// sentEnvelopes returns a copy of all envelopes the client has sent.
func (f *fakeConn) sentEnvelopes() []protocol.Envelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.Envelope, len(f.sent))
	copy(out, f.sent)
	return out
}
