package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/sit/sit/internal/protocol"
)

// DialOptions configures an outbound Node→Manager connection attempt.
type DialOptions struct {
	// Endpoints is an ordered list of wss:// URLs. They are tried in order;
	// the first to connect wins. Not hardcoded — loaded from node config so it
	// can be updated out-of-band (ADR-004).
	Endpoints []string
	// AuthToken is sent as `Authorization: Bearer <token>` during the WS upgrade
	// (enroll_token on first contact, long-term credential afterwards).
	AuthToken string
	// TLSConfig, if set, customizes certificate validation (Node MUST validate
	// the Manager cert in production).
	HTTPClient *http.Client
	// DialTimeout bounds a single endpoint attempt.
	DialTimeout time.Duration
}

// ErrNoEndpoints is returned when DialOptions.Endpoints is empty.
var ErrNoEndpoints = errors.New("transport: no endpoints configured")

// happyEyeballsClient returns an *http.Client whose dialer performs RFC 8305
// Happy Eyeballs. Go's net.Dialer with DualStack (FallbackDelay > 0) races
// IPv4/IPv6 and takes the first to connect (ADR-007).
func happyEyeballsClient(timeout time.Duration) *http.Client {
	d := &net.Dialer{
		Timeout:       timeout,
		FallbackDelay: 300 * time.Millisecond, // enables Happy Eyeballs
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:         d.DialContext,
			TLSHandshakeTimeout: timeout,
		},
	}
}

// Dial tries each endpoint in order and returns the first successful Conn.
// A single endpoint that resolves to both A and AAAA records is handled by
// Happy Eyeballs inside the dialer.
func Dial(ctx context.Context, opts DialOptions) (Conn, error) {
	if len(opts.Endpoints) == 0 {
		return nil, ErrNoEndpoints
	}
	if opts.DialTimeout == 0 {
		opts.DialTimeout = 10 * time.Second
	}
	client := opts.HTTPClient
	if client == nil {
		client = happyEyeballsClient(opts.DialTimeout)
	}

	header := http.Header{}
	if opts.AuthToken != "" {
		header.Set("Authorization", "Bearer "+opts.AuthToken)
	}

	var lastErr error
	for _, ep := range opts.Endpoints {
		attemptCtx, cancel := context.WithTimeout(ctx, opts.DialTimeout)
		ws, _, err := websocket.Dial(attemptCtx, ep, &websocket.DialOptions{
			HTTPClient: client,
			HTTPHeader: header,
		})
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("dial %s: %w", ep, err)
			continue
		}
		return newWSConn(ws, SessionInfo{RemoteAddr: ep, ConnectedAt: protocol.NowMillis()}), nil
	}
	return nil, fmt.Errorf("transport: all endpoints failed: %w", lastErr)
}
