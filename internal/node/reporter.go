package node

import (
	"net"
	"os"
	"runtime"

	"github.com/sit/sit/internal/protocol"
)

// Reporter builds Node→Manager notifications: register, heartbeat, event.
// (result is built by the Executor.) Identity fields are self-reported and are
// NOT an auth basis — the handshake credential is authoritative on the Manager.
type Reporter struct {
	nodeID  string
	version string
	startMS int64
}

// NewReporter constructs a Reporter for a node id and binary version.
func NewReporter(nodeID, version string) *Reporter {
	return &Reporter{nodeID: nodeID, version: version, startMS: protocol.NowMillis()}
}

// Register builds a register notification with host facts and all local addrs.
func (r *Reporter) Register() protocol.Notification {
	host, _ := os.Hostname()
	return protocol.Notification{
		Kind:     protocol.KindRegister,
		NodeID:   r.nodeID,
		Hostname: host,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  r.version,
		Addrs:    enumerateAddrs(),
	}
}

// Heartbeat builds a heartbeat with uptime and memory usage.
func (r *Reporter) Heartbeat() protocol.Notification {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	uptime := (protocol.NowMillis() - r.startMS) / 1000
	return protocol.Notification{
		Kind:      protocol.KindHeartbeat,
		UptimeSec: uptime,
		MemUsedMB: int64(ms.Alloc >> 20),
	}
}

// Event builds an event notification (e.g. online, shutting_down).
func (r *Reporter) Event(event, detail string) protocol.Notification {
	return protocol.Notification{Kind: protocol.KindEvent, Event: event, Detail: detail}
}

// enumerateAddrs lists all non-loopback unicast addresses across interfaces,
// dual-stack (v4/v6) with iface name and scope classification.
func enumerateAddrs() []protocol.Addr {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []protocol.Addr
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP
			if ip == nil || ip.IsUnspecified() {
				continue
			}
			out = append(out, protocol.Addr{
				IP:     ip.String(),
				Family: family(ip),
				Iface:  ifi.Name,
				Scope:  scope(ip),
			})
		}
	}
	return out
}

func family(ip net.IP) string {
	if ip.To4() != nil {
		return "v4"
	}
	return "v6"
}

func scope(ip net.IP) string {
	switch {
	case ip.IsLoopback():
		return "loopback"
	case ip.IsLinkLocalUnicast():
		return "link"
	case ip.IsPrivate():
		return "private"
	default:
		return "global"
	}
}
