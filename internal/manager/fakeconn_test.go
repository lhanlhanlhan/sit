package manager

import (
	"context"
	"sync"

	"github.com/sit/sit/internal/protocol"
	"github.com/sit/sit/internal/transport"
)

// fakeConn is an in-memory transport.Conn for tests. Sent envelopes are
// captured; injected envelopes appear on Recv.
type fakeConn struct {
	mu     sync.Mutex
	sent   []protocol.Envelope
	recv   chan protocol.Envelope
	done   chan struct{}
	info   transport.SessionInfo
	closed bool
}

func newFakeConn(nodeID string) *fakeConn {
	return &fakeConn{
		recv: make(chan protocol.Envelope, 16),
		done: make(chan struct{}),
		info: transport.SessionInfo{NodeID: nodeID, ConnectedAt: protocol.NowMillis()},
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

func (f *fakeConn) Recv() <-chan protocol.Envelope { return f.recv }
func (f *fakeConn) Info() transport.SessionInfo    { return f.info }
func (f *fakeConn) Done() <-chan struct{}          { return f.done }

func (f *fakeConn) Close(reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	return nil
}

func (f *fakeConn) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeConn) lastSent() (protocol.Envelope, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return protocol.Envelope{}, false
	}
	return f.sent[len(f.sent)-1], true
}

func (f *fakeConn) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

var _ transport.Conn = (*fakeConn)(nil)
