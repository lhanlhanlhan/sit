// Package managerd assembles the Manager process: it wires the manager-core
// services (store/auth/registry/dispatcher/reports) to the WSS, REST, and MCP
// listeners. It lives above internal/manager to avoid an import cycle with the
// api and mcp sub-packages (which depend on manager).
package managerd

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/manager/api"
	"github.com/sit/sit/internal/manager/mcp"
	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
	"github.com/sit/sit/internal/transport"
)

// Server is the assembled Manager: store + core services + WSS/REST/MCP listeners.
type Server struct {
	cfg   manager.Config
	store store.Store
	auth  *manager.Auth
	reg   *manager.Registry
	disp  *manager.Dispatcher
	rep   *manager.Reports
}

// NewServer opens the store, seeds the admin (first boot), and wires core services.
func NewServer(cfg manager.Config) (*Server, error) {
	s, err := store.OpenSQLite(cfg.StorePath)
	if err != nil {
		return nil, err
	}
	auth := manager.NewAuth(s)
	if cfg.AdminUser != "" && cfg.AdminPassword != "" {
		if _, err := s.GetAdmin(context.Background(), cfg.AdminUser); errors.Is(err, store.ErrNotFound) {
			if err := auth.SeedAdmin(context.Background(), cfg.AdminUser, cfg.AdminPassword); err != nil {
				return nil, err
			}
		}
	}
	reg := manager.NewRegistry()
	disp := manager.NewDispatcher(s, reg)
	rep := manager.NewReports(s, reg, disp)
	return &Server{cfg: cfg, store: s, auth: auth, reg: reg, disp: disp, rep: rep}, nil
}

// Close releases the store.
func (s *Server) Close() error { return s.store.Close() }

// wssAuth validates a node handshake: bearer token is "<node_id>:<secret>".
// The returned node id is authoritative (never trust self-reported register).
func (s *Server) wssAuth(r *http.Request) (string, error) {
	tok := transport.BearerToken(r)
	nodeID, secret, ok := strings.Cut(tok, ":")
	if !ok || nodeID == "" {
		return "", errors.New("malformed node token")
	}
	if err := s.auth.VerifyNode(r.Context(), nodeID, secret); err != nil {
		return "", err
	}
	return nodeID, nil
}

// handleConn registers the session and pumps inbound notifications into reports.
func (s *Server) handleConn(conn transport.Conn) {
	nodeID := conn.Info().NodeID
	s.reg.Add(nodeID, conn)
	defer s.reg.Remove(nodeID)

	ctx := context.Background()
	for {
		select {
		case <-conn.Done():
			return
		case env, ok := <-conn.Recv():
			if !ok {
				return
			}
			if env.Type != protocol.TypeNotification {
				continue
			}
			n, err := env.AsNotification()
			if err != nil {
				continue
			}
			if err := s.rep.Handle(ctx, nodeID, n); err != nil {
				log.Printf("manager: handle notification from %s: %v", nodeID, err)
			}
		}
	}
}

// apiHandler builds the combined REST (/api/v1) + MCP (/mcp) mux.
func (s *Server) apiHandler() http.Handler {
	mux := http.NewServeMux()
	restH := api.New(api.Deps{Auth: s.auth, Store: s.store, Registry: s.reg, Dispatcher: s.disp}).Handler()
	mcpH := mcp.New(mcp.Deps{Store: s.store, Registry: s.reg, Dispatcher: s.disp, MCPToken: s.cfg.MCPToken}).Handler()
	mux.Handle("/api/v1/", restH)
	mux.Handle("/mcp", mcpH)
	return mux
}

// wssHandler builds the node WSS endpoint at /sit/connect.
func (s *Server) wssHandler() http.Handler {
	srv := &transport.Server{Auth: s.wssAuth, Handler: s.handleConn}
	mux := http.NewServeMux()
	mux.Handle("/sit/connect", srv)
	return mux
}

// Run starts the reaper plus the WSS and API listeners, blocking until ctx is
// cancelled. TLS is used when cert/key are configured.
func (s *Server) Run(ctx context.Context) error {
	go s.reapLoop(ctx)

	wss := &http.Server{Addr: s.cfg.ListenWSS, Handler: s.wssHandler()}
	apiSrv := &http.Server{Addr: s.cfg.ListenAPI, Handler: s.apiHandler()}

	errCh := make(chan error, 2)
	go func() { errCh <- listen(wss, s.cfg) }()
	go func() { errCh <- listen(apiSrv, s.cfg) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = wss.Shutdown(shutCtx)
		_ = apiSrv.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func listen(srv *http.Server, cfg manager.Config) error {
	if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
		return srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	return srv.ListenAndServe()
}

// reapLoop periodically marks stale sessions offline (>90s no frame).
func (s *Server) reapLoop(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := protocol.NowMillis()
			for _, id := range s.reg.ReapStale(now) {
				_ = s.store.SetNodeStatus(ctx, id, "offline", now)
				_ = s.store.AppendActivity(ctx, store.Activity{NodeID: id, Type: "offline", DetailJSON: "{}", At: now})
			}
		}
	}
}
