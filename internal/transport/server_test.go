package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sit/sit/internal/protocol"
)

func TestServer_RejectsBadBearer(t *testing.T) {
	srv := &Server{
		Auth: func(r *http.Request) (string, error) {
			if BearerToken(r) != "good-token" {
				return "", errors.New("bad token")
			}
			return "node-1", nil
		},
		Handler: func(c Conn) { c.Close("test") },
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// No token -> 401, dial fails.
	_, _, err := websocket.Dial(context.Background(), wsURL(ts.URL), nil)
	if err == nil {
		t.Fatal("expected dial to fail without token")
	}
}

func TestServer_AcceptsGoodBearerAndBindsIdentity(t *testing.T) {
	gotID := make(chan string, 1)
	srv := &Server{
		Auth: func(r *http.Request) (string, error) {
			if BearerToken(r) != "good-token" {
				return "", errors.New("bad token")
			}
			return "node-42", nil
		},
		Handler: func(c Conn) {
			gotID <- c.Info().NodeID
			c.Close("done")
		},
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer good-token")
	ws, _, err := websocket.Dial(context.Background(), wsURL(ts.URL), &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		t.Fatalf("dial with good token: %v", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	select {
	case id := <-gotID:
		if id != "node-42" {
			t.Errorf("bound identity: got %q want node-42", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not invoked")
	}
}

func TestServer_EndToEndFrameExchange(t *testing.T) {
	srv := &Server{
		Auth: func(r *http.Request) (string, error) { return "node-x", nil },
		Handler: func(c Conn) {
			// Echo one notification back as an ack.
			select {
			case env := <-c.Recv():
				ack, _ := protocol.NewAck(env.ID)
				_ = c.Send(context.Background(), ack)
			case <-time.After(time.Second):
			}
		},
	}
	ts := httptest.NewServer(srv)
	defer ts.Close()

	ws, _, err := websocket.Dial(context.Background(), wsURL(ts.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := newWSConn(ws, SessionInfo{})
	defer client.Close("done")

	env, _ := protocol.NewNotification(protocol.Notification{Kind: protocol.KindEvent, Event: "online"})
	if err := client.Send(context.Background(), env); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case got := <-client.Recv():
		ack, err := got.AsAck()
		if err != nil {
			t.Fatalf("AsAck: %v", err)
		}
		if ack.RefID != env.ID {
			t.Errorf("ack ref_id: got %q want %q", ack.RefID, env.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no ack received")
	}
}
