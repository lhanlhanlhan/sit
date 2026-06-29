package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type nodeSetupOptions struct {
	Endpoint     string
	WSSEndpoint  string
	Token        string
	ConfigPath   string
	StateDir     string
	LogDir       string
	User         string
	Group        string
	BinPath      string
	ServicePath  string
	HeartbeatSec int
	AssumeYes    bool
}

var (
	setupGOOS          = runtime.GOOS
	setupGetEUID       = os.Geteuid
	setupLookPath      = exec.LookPath
	setupRunCommand    = run
	setupCommandOK     = commandOK
	setupCommandOutput = commandOutput
)

func newNodeSetupCmd() *cobra.Command {
	opts := defaultNodeSetupOptions(setupGOOS)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactively configure this host as a SIT Node",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNodeSetup(cmd.InOrStdin(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.Endpoint, "endpoint", "", "manager API base URL, e.g. https://mgr.example/sit")
	cmd.Flags().StringVar(&opts.WSSEndpoint, "wss-endpoint", "", "node WSS endpoint, e.g. wss://mgr.example/sit/connect")
	cmd.Flags().StringVar(&opts.Token, "token", "", "one-time enrollment token")
	cmd.Flags().StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "node config path")
	cmd.Flags().StringVar(&opts.StateDir, "state-dir", opts.StateDir, "node state directory")
	cmd.Flags().StringVar(&opts.LogDir, "log-dir", opts.LogDir, "node log directory (macOS launchd)")
	cmd.Flags().StringVar(&opts.User, "user", opts.User, "system user to run the node")
	cmd.Flags().StringVar(&opts.Group, "group", opts.Group, "system group to run the node")
	cmd.Flags().StringVar(&opts.BinPath, "bin", opts.BinPath, "system path for the sit binary")
	cmd.Flags().StringVar(&opts.ServicePath, "service", opts.ServicePath, "service file path")
	cmd.Flags().IntVar(&opts.HeartbeatSec, "heartbeat-sec", opts.HeartbeatSec, "heartbeat interval in seconds")
	cmd.Flags().BoolVarP(&opts.AssumeYes, "yes", "y", false, "accept defaults and overwrite managed files")
	return cmd
}

func defaultNodeSetupOptions(goos string) nodeSetupOptions {
	opts := nodeSetupOptions{
		ConfigPath:   "/etc/sit/node.yaml",
		StateDir:     "/var/lib/sit",
		User:         "sit",
		Group:        "sit",
		BinPath:      "/usr/local/bin/sit",
		ServicePath:  "/etc/systemd/system/sit-node.service",
		HeartbeatSec: 30,
	}
	if goos == "darwin" {
		opts.ConfigPath = "/usr/local/etc/sit/node.yaml"
		opts.StateDir = "/usr/local/var/lib/sit"
		opts.LogDir = "/usr/local/var/log/sit"
		opts.User = "_sit"
		opts.Group = "_sit"
		opts.ServicePath = "/Library/LaunchDaemons/com.meating.sit.plist"
	}
	return opts
}

func runNodeSetup(in io.Reader, out io.Writer, opts nodeSetupOptions) error {
	platform := setupGOOS
	if platform != "linux" && platform != "darwin" {
		return errors.New("node setup currently supports Linux/systemd and macOS/launchd only")
	}
	if setupGetEUID() != 0 {
		return errors.New("node setup must run as root; use sudo")
	}
	if platform == "linux" {
		if _, err := setupLookPath("systemctl"); err != nil {
			return errors.New("node setup requires systemd: systemctl not found")
		}
	}
	if platform == "darwin" {
		if _, err := setupLookPath("launchctl"); err != nil {
			return errors.New("node setup requires launchd: launchctl not found")
		}
		if _, err := setupLookPath("dscl"); err != nil {
			return errors.New("node setup requires dscl")
		}
	}
	if opts.Group == "" {
		opts.Group = opts.User
	}

	p := &setupPrompter{in: bufio.NewReader(in), out: out, assumeYes: opts.AssumeYes}
	var err error
	opts.Endpoint, err = p.promptRequired("Manager API address", opts.Endpoint)
	if err != nil {
		return err
	}
	opts.ConfigPath, err = p.promptDefault("Node config path", opts.ConfigPath)
	if err != nil {
		return err
	}
	opts.StateDir, err = p.promptDefault("Node state directory", opts.StateDir)
	if err != nil {
		return err
	}
	if platform == "darwin" {
		opts.LogDir, err = p.promptDefault("Node log directory", opts.LogDir)
		if err != nil {
			return err
		}
	}
	opts.User, err = p.promptDefault("System user", opts.User)
	if err != nil {
		return err
	}
	if platform == "darwin" {
		opts.Group, err = p.promptDefault("System group", opts.Group)
		if err != nil {
			return err
		}
	} else {
		opts.Group = opts.User
	}
	opts.BinPath, err = p.promptDefault("Install sit binary to", opts.BinPath)
	if err != nil {
		return err
	}
	opts.ServicePath, err = p.promptDefault(servicePathLabel(platform), opts.ServicePath)
	if err != nil {
		return err
	}
	if opts.HeartbeatSec <= 0 {
		opts.HeartbeatSec = 30
	}
	if !identityHasSecret(filepath.Join(opts.StateDir, "identity.json")) {
		opts.Token, err = p.promptRequired("Enrollment token", opts.Token)
		if err != nil {
			return err
		}
	}

	endpoint := strings.TrimRight(opts.Endpoint, "/")
	if opts.WSSEndpoint == "" {
		opts.WSSEndpoint = managerAPIToWSS(endpoint)
	}
	opts.WSSEndpoint, err = p.promptDefault("Node WSS endpoint", opts.WSSEndpoint)
	if err != nil {
		return err
	}
	wssEndpoint := strings.TrimRight(opts.WSSEndpoint, "/")
	fmt.Fprintf(out, "sit setup: WSS endpoint will be %s\n", wssEndpoint)

	if platform == "darwin" {
		if err := ensureDarwinSystemUser(opts.User, opts.Group, out); err != nil {
			return err
		}
	} else {
		if err := ensureSystemUser(opts.User, opts.StateDir, out); err != nil {
			return err
		}
	}
	if err := installSelf(opts.BinPath, out); err != nil {
		return err
	}
	if platform == "darwin" {
		if err := setupRunCommand(out, "chown", "root:wheel", opts.BinPath); err != nil {
			return err
		}
		if err := os.Chmod(opts.BinPath, 0755); err != nil {
			return err
		}
		if err := ensureOwnedDirForGroup(opts.StateDir, opts.User, opts.Group, 0700, out); err != nil {
			return err
		}
		if err := ensureOwnedDirForGroup(opts.LogDir, opts.User, opts.Group, 0750, out); err != nil {
			return err
		}
	} else {
		if err := ensureOwnedDir(opts.StateDir, opts.User, out); err != nil {
			return err
		}
	}
	if err := writeNodeConfig(opts.ConfigPath, opts.StateDir, wssEndpoint, opts.HeartbeatSec, p); err != nil {
		return err
	}
	if err := enrollAsUser(opts.User, opts.BinPath, opts.Token, endpoint, opts.StateDir, out); err != nil {
		return err
	}
	if platform == "darwin" {
		if err := installLaunchDaemon(opts, p, out); err != nil {
			return err
		}
	} else {
		if err := writeNodeService(opts.ServicePath, opts.User, opts.BinPath, opts.ConfigPath, p); err != nil {
			return err
		}
		if err := setupRunCommand(out, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := setupRunCommand(out, "systemctl", "enable", "--now", filepath.Base(opts.ServicePath)); err != nil {
			return err
		}
	}

	fmt.Fprintln(out, "sit setup: done")
	if platform == "darwin" {
		fmt.Fprintf(out, "sit setup: check status with `launchctl print system/%s`\n", launchdLabel)
	} else {
		fmt.Fprintf(out, "sit setup: check status with `systemctl status %s`\n", filepath.Base(opts.ServicePath))
	}
	return nil
}

func servicePathLabel(platform string) string {
	if platform == "darwin" {
		return "launchd plist path"
	}
	return "systemd service path"
}

type setupPrompter struct {
	in        *bufio.Reader
	out       io.Writer
	assumeYes bool
}

func (p *setupPrompter) promptDefault(label, def string) (string, error) {
	if p.assumeYes {
		return def, nil
	}
	fmt.Fprintf(p.out, "%s [%s]: ", label, def)
	s, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	return s, nil
}

func (p *setupPrompter) promptRequired(label, def string) (string, error) {
	if def != "" {
		return strings.TrimSpace(def), nil
	}
	for {
		fmt.Fprintf(p.out, "%s: ", label)
		s, err := p.in.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		s = strings.TrimSpace(s)
		if s != "" {
			return s, nil
		}
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s is required", label)
		}
	}
}

func (p *setupPrompter) confirmOverwrite(path string) error {
	if p.assumeYes {
		return nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	fmt.Fprintf(p.out, "%s already exists. Overwrite? [y/N]: ", path)
	s, err := p.in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return nil
	default:
		return fmt.Errorf("refusing to overwrite %s", path)
	}
}

func managerAPIToWSS(endpoint string) string {
	wss := endpoint
	if strings.HasPrefix(wss, "https://") {
		wss = "wss://" + strings.TrimPrefix(wss, "https://")
	} else if strings.HasPrefix(wss, "http://") {
		wss = "ws://" + strings.TrimPrefix(wss, "http://")
	}
	wss = strings.TrimRight(wss, "/")
	if strings.HasSuffix(wss, "/sit") {
		return wss + "/connect"
	}
	return wss + "/sit/connect"
}

func ensureSystemUser(user, home string, out io.Writer) error {
	if setupCommandOK("id", "-u", user) {
		fmt.Fprintf(out, "sit setup: user %s already exists\n", user)
		return nil
	}
	return setupRunCommand(out, "useradd", "--system", "--home", home, "--create-home", "--shell", "/usr/sbin/nologin", user)
}

func ensureDarwinSystemUser(user, group string, out io.Writer) error {
	if !setupCommandOK("dscl", ".", "-read", "/Groups/"+group) {
		gid, err := firstFreeDarwinID("/Groups", "PrimaryGroupID")
		if err != nil {
			return err
		}
		if err := setupRunCommand(out, "dscl", ".", "-create", "/Groups/"+group); err != nil {
			return err
		}
		if err := setupRunCommand(out, "dscl", ".", "-create", "/Groups/"+group, "PrimaryGroupID", strconv.Itoa(gid)); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "sit setup: group %s already exists\n", group)
	}
	if setupCommandOK("dscl", ".", "-read", "/Users/"+user) {
		fmt.Fprintf(out, "sit setup: user %s already exists\n", user)
		return nil
	}

	uid, err := firstFreeDarwinID("/Users", "UniqueID")
	if err != nil {
		return err
	}
	gid, err := darwinGroupID(group)
	if err != nil {
		return err
	}
	cmds := [][]string{
		{"dscl", ".", "-create", "/Users/" + user},
		{"dscl", ".", "-create", "/Users/" + user, "UserShell", "/usr/bin/false"},
		{"dscl", ".", "-create", "/Users/" + user, "RealName", "SIT Node"},
		{"dscl", ".", "-create", "/Users/" + user, "UniqueID", strconv.Itoa(uid)},
		{"dscl", ".", "-create", "/Users/" + user, "PrimaryGroupID", strconv.Itoa(gid)},
		{"dscl", ".", "-create", "/Users/" + user, "NFSHomeDirectory", "/var/empty"},
	}
	for _, cmd := range cmds {
		if err := setupRunCommand(out, cmd[0], cmd[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func firstFreeDarwinID(recordType, attr string) (int, error) {
	out, err := setupCommandOutput("dscl", ".", "-list", recordType, attr)
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.Atoi(fields[len(fields)-1])
		if err == nil {
			used[n] = true
		}
	}
	for id := 401; id <= 499; id++ {
		if !used[id] {
			return id, nil
		}
	}
	for id := 200; id <= 999; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no free macOS id for %s", recordType)
}

func darwinGroupID(group string) (int, error) {
	out, err := setupCommandOutput("dscl", ".", "-read", "/Groups/"+group, "PrimaryGroupID")
	if err != nil {
		return 0, err
	}
	for _, field := range strings.Fields(out) {
		if n, err := strconv.Atoi(field); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("group %s has no PrimaryGroupID", group)
}

func installSelf(dest string, out io.Writer) error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	src, _ = filepath.EvalSymlinks(src)
	destAbs, _ := filepath.Abs(dest)
	destEval, _ := filepath.EvalSymlinks(destAbs)
	if src == destEval {
		fmt.Fprintf(out, "sit setup: binary already installed at %s\n", dest)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	outf, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outf, in); err != nil {
		outf.Close()
		return err
	}
	if err := outf.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	fmt.Fprintf(out, "sit setup: installed binary to %s\n", dest)
	return nil
}

func ensureOwnedDir(dir, user string, out io.Writer) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	group := user
	if g, err := setupCommandOutput("id", "-gn", user); err == nil && strings.TrimSpace(g) != "" {
		group = strings.TrimSpace(g)
	}
	if err := setupRunCommand(out, "chown", "-R", user+":"+group, dir); err != nil {
		return err
	}
	return os.Chmod(dir, 0700)
}

func ensureOwnedDirForGroup(dir, user, group string, perm os.FileMode, out io.Writer) error {
	if err := ensureDarwinDirParents(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, perm); err != nil {
		return err
	}
	if err := setupRunCommand(out, "chown", "-R", user+":"+group, dir); err != nil {
		return err
	}
	return os.Chmod(dir, perm)
}

func ensureDarwinDirParents(dir string) error {
	return ensureDirParents(dir, "/usr/local/var")
}

func ensureDirParents(dir, repairRoot string) error {
	parent := filepath.Dir(filepath.Clean(dir))
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	repairRoot = filepath.Clean(repairRoot)
	for p := parent; p != "." && p != string(filepath.Separator); p = filepath.Dir(p) {
		if p == repairRoot || strings.HasPrefix(p, repairRoot+string(filepath.Separator)) {
			if err := os.Chmod(p, 0755); err != nil {
				return err
			}
		}
		if p == repairRoot {
			break
		}
	}
	return nil
}

func writeNodeConfig(path, stateDir, endpoint string, heartbeat int, p *setupPrompter) error {
	if err := p.confirmOverwrite(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	body := fmt.Sprintf(`endpoints:
  - %s
state_dir: %s
heartbeat_sec: %d
insecure_skip_verify: false
`, endpoint, stateDir, heartbeat)
	return os.WriteFile(path, []byte(body), 0644)
}

func enrollAsUser(user, bin, token, endpoint, stateDir string, out io.Writer) error {
	if identityHasSecret(filepath.Join(stateDir, "identity.json")) {
		fmt.Fprintln(out, "sit setup: node identity already enrolled")
		return nil
	}
	if setupGOOS == "linux" && setupCommandOK("runuser", "--help") {
		return setupRunCommand(out, "runuser", "-u", user, "--", bin, "node", "enroll", "--token", token, "--endpoint", endpoint, "--state-dir", stateDir)
	}
	return setupRunCommand(out, "sudo", "-u", user, bin, "node", "enroll", "--token", token, "--endpoint", endpoint, "--state-dir", stateDir)
}

func identityHasSecret(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "secret=") && strings.TrimSpace(strings.TrimPrefix(line, "secret=")) != "" {
			return true
		}
	}
	return false
}

func writeNodeService(path, user, bin, config string, p *setupPrompter) error {
	if err := p.confirmOverwrite(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	body := fmt.Sprintf(`[Unit]
Description=SIT Node
Documentation=https://github.com/lhanlhanlhan/sit
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s node --config %s
Restart=always
RestartSec=5
User=%s
Group=%s

[Install]
WantedBy=multi-user.target
`, bin, config, user, user)
	return os.WriteFile(path, []byte(body), 0644)
}

const launchdLabel = "com.meating.sit"

func installLaunchDaemon(opts nodeSetupOptions, p *setupPrompter, out io.Writer) error {
	if err := writeLaunchdPlist(opts.ServicePath, opts.User, opts.Group, opts.BinPath, opts.ConfigPath, opts.LogDir, p); err != nil {
		return err
	}
	if err := setupRunCommand(out, "chown", "root:wheel", opts.ServicePath); err != nil {
		return err
	}
	if err := os.Chmod(opts.ServicePath, 0644); err != nil {
		return err
	}
	if setupCommandOK("launchctl", "print", "system/"+launchdLabel) {
		if err := setupRunCommand(out, "launchctl", "bootout", "system", opts.ServicePath); err != nil {
			return err
		}
	}
	if err := setupRunCommand(out, "launchctl", "bootstrap", "system", opts.ServicePath); err != nil {
		return err
	}
	if err := setupRunCommand(out, "launchctl", "enable", "system/"+launchdLabel); err != nil {
		return err
	}
	return setupRunCommand(out, "launchctl", "kickstart", "-k", "system/"+launchdLabel)
}

func writeLaunchdPlist(path, user, group, bin, config, logDir string, p *setupPrompter) error {
	if err := p.confirmOverwrite(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(launchdPlistBody(user, group, bin, config, logDir)), 0644)
}

func launchdPlistBody(user, group, bin, config, logDir string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>node</string>
        <string>--config</string>
        <string>%s</string>
    </array>
    <key>UserName</key>
    <string>%s</string>
    <key>GroupName</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>5</integer>
    <key>StandardOutPath</key>
    <string>%s/node.log</string>
    <key>StandardErrorPath</key>
    <string>%s/node.err.log</string>
</dict>
</plist>
`, launchdLabel, bin, config, user, group, logDir, logDir)
}

func run(out io.Writer, name string, args ...string) error {
	fmt.Fprintf(out, "sit setup: running %s %s\n", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	return cmd.Run()
}

func commandOK(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	return cmd.Run() == nil
}

func commandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	b, err := cmd.Output()
	return string(b), err
}
