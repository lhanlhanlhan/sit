package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func wsEcho(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	ctx := context.Background()
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		_ = ws.Write(ctx, typ, data)
	}
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func TestDial_NoEndpoints(t *testing.T) {
	_, err := Dial(context.Background(), DialOptions{})
	if err != ErrNoEndpoints {
		t.Fatalf("got %v want ErrNoEndpoints", err)
	}
}

func TestDial_FailoverToNextEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(wsEcho))
	defer srv.Close()

	opts := DialOptions{
		// First endpoint is a black-hole port that refuses connections; dialer
		// must fail over to the working second endpoint.
		Endpoints:   []string{"ws://127.0.0.1:1/sit/connect", wsURL(srv.URL)},
		DialTimeout: 2 * time.Second,
	}
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer c.Close("done")
	if c.Info().RemoteAddr != wsURL(srv.URL) {
		t.Errorf("connected to %q, expected failover target %q", c.Info().RemoteAddr, wsURL(srv.URL))
	}
}

func TestDial_AllEndpointsFail(t *testing.T) {
	opts := DialOptions{
		Endpoints:   []string{"ws://127.0.0.1:1/x", "ws://127.0.0.1:2/x"},
		DialTimeout: 500 * time.Millisecond,
	}
	_, err := Dial(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "all endpoints failed") {
		t.Fatalf("expected all-endpoints-failed error, got %v", err)
	}
}
