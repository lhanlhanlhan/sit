package manager

import (
	"context"
	"encoding/json"

	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
)

// Reports routes inbound notifications into registry, store, and dispatcher.
// The authoritative node identity is the handshake-bound id (authNodeID),
// never the self-reported register payload.
type Reports struct {
	store      store.Store
	registry   *Registry
	dispatcher *Dispatcher
}

// NewReports constructs a Reports router.
func NewReports(s store.Store, r *Registry, d *Dispatcher) *Reports {
	return &Reports{store: s, registry: r, dispatcher: d}
}

// Handle dispatches one notification for the given authenticated node id.
func (rp *Reports) Handle(ctx context.Context, authNodeID string, n protocol.Notification) error {
	now := protocol.NowMillis()
	rp.registry.Touch(authNodeID, now)

	switch n.Kind {
	case protocol.KindRegister:
		return rp.handleRegister(ctx, authNodeID, n, now)
	case protocol.KindHeartbeat:
		rp.registry.SetHeartbeat(authNodeID, n, now)
		return rp.store.SetNodeStatus(ctx, authNodeID, "online", now)
	case protocol.KindResult:
		return rp.dispatcher.HandleResult(ctx, n)
	case protocol.KindEvent:
		return rp.handleEvent(ctx, authNodeID, n, now)
	default:
		return nil // unknown kind: ignore (forward-compatible)
	}
}

func (rp *Reports) handleRegister(ctx context.Context, nodeID string, n protocol.Notification, now int64) error {
	addrsJSON := "[]"
	if len(n.Addrs) > 0 {
		if b, err := json.Marshal(n.Addrs); err == nil {
			addrsJSON = string(b)
		}
	}
	// Preserve existing display_name/mcp_enabled/created_at if the node exists.
	existing, err := rp.store.GetNode(ctx, nodeID)
	created := now
	display := ""
	if err == nil {
		created = existing.CreatedAt
		display = existing.DisplayName
	}
	node := store.Node{
		NodeID:      nodeID, // authoritative, NOT n.NodeID
		DisplayName: display,
		OS:          n.OS,
		Arch:        n.Arch,
		Version:     n.Version,
		Hostname:    n.Hostname,
		AddrsJSON:   addrsJSON,
		Status:      "online",
		LastSeen:    now,
		CreatedAt:   created,
	}
	if err := rp.store.UpsertNode(ctx, node); err != nil {
		return err
	}
	rp.appendActivity(ctx, nodeID, "register", map[string]any{"os": n.OS, "arch": n.Arch, "version": n.Version})
	rp.appendActivity(ctx, nodeID, "online", nil)
	// Deliver any queued instructions that accumulated while offline.
	return rp.dispatcher.FlushQueue(ctx, nodeID)
}

func (rp *Reports) handleEvent(ctx context.Context, nodeID string, n protocol.Notification, now int64) error {
	rp.appendActivity(ctx, nodeID, "event", map[string]any{"event": n.Event, "detail": n.Detail})
	if n.Event == "shutting_down" {
		return rp.store.SetNodeStatus(ctx, nodeID, "offline", now)
	}
	return nil
}

func (rp *Reports) appendActivity(ctx context.Context, nodeID, typ string, detail map[string]any) {
	b, _ := json.Marshal(detail)
	_ = rp.store.AppendActivity(ctx, store.Activity{
		NodeID:     nodeID,
		Type:       typ,
		DetailJSON: string(b),
		At:         protocol.NowMillis(),
	})
}
