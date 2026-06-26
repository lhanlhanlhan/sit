package node

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sit/sit/internal/protocol"
)

func TestExecutor_ShellCapturesOutputAndExit(t *testing.T) {
	e := NewExecutor(nil)
	res := e.Execute(context.Background(), "i1", protocol.Instruction{
		Kind: protocol.KindShell, Command: "echo out; echo err 1>&2; exit 0", TimeoutSec: 10,
	})
	if res.Kind != protocol.KindResult || res.RefID != "i1" {
		t.Fatalf("result envelope: %+v", res)
	}
	if !strings.Contains(res.Stdout, "out") {
		t.Errorf("stdout: %q", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "err") {
		t.Errorf("stderr: %q", res.Stderr)
	}
	if res.ExitCode != 0 {
		t.Errorf("exit code: %d", res.ExitCode)
	}
}

func TestExecutor_NonZeroExit(t *testing.T) {
	e := NewExecutor(nil)
	res := e.Execute(context.Background(), "i1", protocol.Instruction{Kind: protocol.KindShell, Command: "exit 3", TimeoutSec: 10})
	if res.ExitCode != 3 {
		t.Errorf("exit code: got %d want 3", res.ExitCode)
	}
}

func TestExecutor_TruncatesLargeOutput(t *testing.T) {
	e := NewExecutor(nil)
	// Produce ~128KB of output.
	res := e.Execute(context.Background(), "i1", protocol.Instruction{
		Kind: protocol.KindShell, Command: "head -c 131072 /dev/zero | tr '\\0' 'a'", TimeoutSec: 10,
	})
	if !res.Truncated {
		t.Error("output should be truncated")
	}
	if len(res.Stdout) != maxOutputBytes {
		t.Errorf("stdout len: got %d want %d", len(res.Stdout), maxOutputBytes)
	}
}

func TestExecutor_TimeoutKills(t *testing.T) {
	e := NewExecutor(nil)
	res := e.Execute(context.Background(), "i1", protocol.Instruction{Kind: protocol.KindShell, Command: "sleep 30", TimeoutSec: 1})
	if !res.TimedOut {
		t.Errorf("should time out: %+v", res)
	}
}

func TestExecutor_DeadlineExpired(t *testing.T) {
	e := NewExecutor(nil)
	res := e.Execute(context.Background(), "i1", protocol.Instruction{
		Kind: protocol.KindShell, Command: "echo hi", Deadline: protocol.NowMillis() - 1000,
	})
	if res.ExitCode != -1 || !strings.Contains(res.Stderr, "deadline") {
		t.Errorf("expired result: %+v", res)
	}
}

func TestExecutor_DedupReplays(t *testing.T) {
	e := NewExecutor(nil)
	first := e.Execute(context.Background(), "i1", protocol.Instruction{Kind: protocol.KindShell, Command: "echo first", TimeoutSec: 10})
	// Same id, different command — must replay the FIRST result.
	second := e.Execute(context.Background(), "i1", protocol.Instruction{Kind: protocol.KindShell, Command: "echo second", TimeoutSec: 10})
	if second.Stdout != first.Stdout {
		t.Errorf("dedup failed: %q vs %q", second.Stdout, first.Stdout)
	}
}

func TestExecutor_Predefined(t *testing.T) {
	e := NewExecutor(map[string]PredefinedFunc{
		"ping": func(ctx context.Context, args map[string]any) (string, int, error) {
			return "pong", 0, nil
		},
		"boom": func(ctx context.Context, args map[string]any) (string, int, error) {
			return "", 1, errors.New("kaboom")
		},
	})
	ok := e.Execute(context.Background(), "i1", protocol.Instruction{Kind: protocol.KindPredefined, Name: "ping"})
	if ok.Stdout != "pong" || ok.ExitCode != 0 {
		t.Errorf("predefined ping: %+v", ok)
	}
	bad := e.Execute(context.Background(), "i2", protocol.Instruction{Kind: protocol.KindPredefined, Name: "boom"})
	if bad.ExitCode != 1 || !strings.Contains(bad.Stderr, "kaboom") {
		t.Errorf("predefined boom: %+v", bad)
	}
	unk := e.Execute(context.Background(), "i3", protocol.Instruction{Kind: protocol.KindPredefined, Name: "nope"})
	if unk.ExitCode != -1 {
		t.Errorf("unknown predefined should fail: %+v", unk)
	}
}
