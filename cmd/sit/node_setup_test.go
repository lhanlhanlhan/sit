package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type recordedCommand struct {
	name string
	args []string
}

func withSetupFakes(t *testing.T, goos string, fakeOK func(string, ...string) bool, fakeOutput func(string, ...string) (string, error), fakeRun func(io.Writer, string, ...string) error) {
	t.Helper()
	oldGOOS := setupGOOS
	oldGetEUID := setupGetEUID
	oldLookPath := setupLookPath
	oldRun := setupRunCommand
	oldOK := setupCommandOK
	oldOutput := setupCommandOutput
	setupGOOS = goos
	setupGetEUID = func() int { return 0 }
	setupLookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	setupRunCommand = fakeRun
	setupCommandOK = fakeOK
	setupCommandOutput = fakeOutput
	t.Cleanup(func() {
		setupGOOS = oldGOOS
		setupGetEUID = oldGetEUID
		setupLookPath = oldLookPath
		setupRunCommand = oldRun
		setupCommandOK = oldOK
		setupCommandOutput = oldOutput
	})
}

func TestDefaultNodeSetupOptionsDarwin(t *testing.T) {
	opts := defaultNodeSetupOptions("darwin")
	if opts.User != "_sit" || opts.Group != "_sit" {
		t.Fatalf("darwin user/group = %s/%s, want _sit/_sit", opts.User, opts.Group)
	}
	if opts.ConfigPath != "/usr/local/etc/sit/node.yaml" {
		t.Fatalf("darwin config = %s", opts.ConfigPath)
	}
	if opts.ServicePath != "/Library/LaunchDaemons/com.meating.sit.plist" {
		t.Fatalf("darwin service = %s", opts.ServicePath)
	}
}

func TestWriteNodeServiceKeepsLinuxUnitShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sit-node.service")
	p := &setupPrompter{out: io.Discard, assumeYes: true}
	if err := writeNodeService(path, "sit", "/usr/local/bin/sit", "/etc/sit/node.yaml", p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := `[Unit]
Description=SIT Node
Documentation=https://github.com/lhanlhanlhan/sit
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/sit node --config /etc/sit/node.yaml
Restart=always
RestartSec=5
User=sit
Group=sit

[Install]
WantedBy=multi-user.target
`
	if got != want {
		t.Fatalf("linux unit changed unexpectedly:\n%s", got)
	}
}

func TestLaunchdPlistUsesDedicatedUserAndMeatingLabel(t *testing.T) {
	got := launchdPlistBody("_sit", "_sit", "/usr/local/bin/sit", "/usr/local/etc/sit/node.yaml", "/usr/local/var/log/sit")
	for _, want := range []string{
		"<string>com.meating.sit</string>",
		"<key>UserName</key>\n    <string>_sit</string>",
		"<key>GroupName</key>\n    <string>_sit</string>",
		"<string>/usr/local/bin/sit</string>",
		"<string>/usr/local/etc/sit/node.yaml</string>",
		"<string>/usr/local/var/log/sit/node.log</string>",
		"<string>/usr/local/var/log/sit/node.err.log</string>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("plist missing %q:\n%s", want, got)
		}
	}
}

func TestRunNodeSetupDarwinInstallsLaunchDaemon(t *testing.T) {
	dir := t.TempDir()
	var commands []recordedCommand
	withSetupFakes(t, "darwin",
		func(name string, args ...string) bool {
			if name == "launchctl" && reflect.DeepEqual(args, []string{"print", "system/" + launchdLabel}) {
				return false
			}
			return false
		},
		func(name string, args ...string) (string, error) {
			if name != "dscl" {
				return "", errors.New("unexpected command output")
			}
			joined := strings.Join(args, " ")
			switch joined {
			case ". -list /Groups PrimaryGroupID":
				return "_daemon 1\n_www 70\n", nil
			case ". -list /Users UniqueID":
				return "_daemon 1\n_www 70\n", nil
			case ". -read /Groups/_sit PrimaryGroupID":
				return "PrimaryGroupID: 401\n", nil
			default:
				return "", errors.New("unexpected dscl output: " + joined)
			}
		},
		func(out io.Writer, name string, args ...string) error {
			commands = append(commands, recordedCommand{name: name, args: append([]string(nil), args...)})
			return nil
		},
	)

	opts := nodeSetupOptions{
		Endpoint:     "https://mgr.example/sit",
		Token:        "enroll-token",
		ConfigPath:   filepath.Join(dir, "etc", "node.yaml"),
		StateDir:     filepath.Join(dir, "var", "lib", "sit"),
		LogDir:       filepath.Join(dir, "var", "log", "sit"),
		User:         "_sit",
		Group:        "_sit",
		BinPath:      filepath.Join(dir, "bin", "sit"),
		ServicePath:  filepath.Join(dir, "LaunchDaemons", "com.meating.sit.plist"),
		HeartbeatSec: 30,
		AssumeYes:    true,
	}
	var out bytes.Buffer
	if err := runNodeSetup(strings.NewReader(""), &out, opts); err != nil {
		t.Fatal(err)
	}

	cfg, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), "state_dir: "+opts.StateDir) {
		t.Fatalf("config state dir mismatch:\n%s", cfg)
	}
	if !strings.Contains(string(cfg), "  - wss://mgr.example/sit/connect") {
		t.Fatalf("config endpoint mismatch:\n%s", cfg)
	}
	plist, err := os.ReadFile(opts.ServicePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plist), "<string>com.meating.sit</string>") {
		t.Fatalf("plist label mismatch:\n%s", plist)
	}
	assertCommand(t, commands, "sudo", "-u", "_sit", opts.BinPath, "node", "enroll", "--token", "enroll-token", "--endpoint", "https://mgr.example/sit", "--state-dir", opts.StateDir)
	assertCommand(t, commands, "launchctl", "bootstrap", "system", opts.ServicePath)
	assertCommand(t, commands, "launchctl", "enable", "system/"+launchdLabel)
	assertCommand(t, commands, "launchctl", "kickstart", "-k", "system/"+launchdLabel)
}

func TestEnsureOwnedDirForGroupKeepsParentsTraversable(t *testing.T) {
	root := filepath.Join(t.TempDir(), "usr", "local", "var")
	dir := filepath.Join(root, "lib", "sit")
	var commands []recordedCommand
	withSetupFakes(t, "darwin",
		func(name string, args ...string) bool { return false },
		func(name string, args ...string) (string, error) { return "", nil },
		func(out io.Writer, name string, args ...string) error {
			commands = append(commands, recordedCommand{name: name, args: append([]string(nil), args...)})
			return nil
		},
	)

	if err := ensureOwnedDirForGroup(dir, "_sit", "_sit", 0700, io.Discard); err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Stat(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if got := parentInfo.Mode().Perm(); got != 0755 {
		t.Fatalf("parent mode = %o, want 0755", got)
	}
	leafInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := leafInfo.Mode().Perm(); got != 0700 {
		t.Fatalf("leaf mode = %o, want 0700", got)
	}
	assertCommand(t, commands, "chown", "-R", "_sit:_sit", dir)
}

func TestEnsureDirParentsRepairsExistingNarrowParents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "usr", "local", "var")
	parent := filepath.Join(root, "lib")
	dir := filepath.Join(parent, "sit")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ensureDirParents(dir, root); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, parent} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0755 {
			t.Fatalf("%s mode = %o, want 0755", path, got)
		}
	}
}

func assertCommand(t *testing.T, commands []recordedCommand, name string, args ...string) {
	t.Helper()
	for _, cmd := range commands {
		if cmd.name == name && reflect.DeepEqual(cmd.args, args) {
			return
		}
	}
	t.Fatalf("missing command %s %s; got %#v", name, strings.Join(args, " "), commands)
}
