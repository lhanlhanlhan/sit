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
	"strings"

	"github.com/spf13/cobra"
)

type nodeSetupOptions struct {
	Endpoint     string
	WSSEndpoint  string
	Token        string
	ConfigPath   string
	StateDir     string
	User         string
	BinPath      string
	ServicePath  string
	HeartbeatSec int
	AssumeYes    bool
}

func newNodeSetupCmd() *cobra.Command {
	opts := nodeSetupOptions{
		ConfigPath:   "/etc/sit/node.yaml",
		StateDir:     "/var/lib/sit",
		User:         "sit",
		BinPath:      "/usr/local/bin/sit",
		ServicePath:  "/etc/systemd/system/sit-node.service",
		HeartbeatSec: 30,
	}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactively configure this Linux host as a SIT Node",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNodeSetup(cmd.InOrStdin(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.Endpoint, "endpoint", "", "manager API base URL, e.g. https://mgr.example/sit")
	cmd.Flags().StringVar(&opts.WSSEndpoint, "wss-endpoint", "", "node WSS endpoint, e.g. wss://mgr.example/sit/connect")
	cmd.Flags().StringVar(&opts.Token, "token", "", "one-time enrollment token")
	cmd.Flags().StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "node config path")
	cmd.Flags().StringVar(&opts.StateDir, "state-dir", opts.StateDir, "node state directory")
	cmd.Flags().StringVar(&opts.User, "user", opts.User, "system user to run the node")
	cmd.Flags().StringVar(&opts.BinPath, "bin", opts.BinPath, "system path for the sit binary")
	cmd.Flags().StringVar(&opts.ServicePath, "service", opts.ServicePath, "systemd service file path")
	cmd.Flags().IntVar(&opts.HeartbeatSec, "heartbeat-sec", opts.HeartbeatSec, "heartbeat interval in seconds")
	cmd.Flags().BoolVarP(&opts.AssumeYes, "yes", "y", false, "accept defaults and overwrite managed files")
	return cmd
}

func runNodeSetup(in io.Reader, out io.Writer, opts nodeSetupOptions) error {
	if runtime.GOOS != "linux" {
		return errors.New("node setup currently supports Linux/systemd only")
	}
	if os.Geteuid() != 0 {
		return errors.New("node setup must run as root; use sudo")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errors.New("node setup requires systemd: systemctl not found")
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
	opts.User, err = p.promptDefault("System user", opts.User)
	if err != nil {
		return err
	}
	opts.BinPath, err = p.promptDefault("Install sit binary to", opts.BinPath)
	if err != nil {
		return err
	}
	opts.ServicePath, err = p.promptDefault("systemd service path", opts.ServicePath)
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

	if err := ensureSystemUser(opts.User, opts.StateDir, out); err != nil {
		return err
	}
	if err := installSelf(opts.BinPath, out); err != nil {
		return err
	}
	if err := ensureOwnedDir(opts.StateDir, opts.User, out); err != nil {
		return err
	}
	if err := writeNodeConfig(opts.ConfigPath, opts.StateDir, wssEndpoint, opts.HeartbeatSec, p); err != nil {
		return err
	}
	if err := enrollAsUser(opts.User, opts.BinPath, opts.Token, endpoint, opts.StateDir, out); err != nil {
		return err
	}
	if err := writeNodeService(opts.ServicePath, opts.User, opts.BinPath, opts.ConfigPath, p); err != nil {
		return err
	}
	if err := run(out, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if err := run(out, "systemctl", "enable", "--now", filepath.Base(opts.ServicePath)); err != nil {
		return err
	}

	fmt.Fprintln(out, "sit setup: done")
	fmt.Fprintf(out, "sit setup: check status with `systemctl status %s`\n", filepath.Base(opts.ServicePath))
	return nil
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
	if commandOK("id", "-u", user) {
		fmt.Fprintf(out, "sit setup: user %s already exists\n", user)
		return nil
	}
	return run(out, "useradd", "--system", "--home", home, "--create-home", "--shell", "/usr/sbin/nologin", user)
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
	if g, err := commandOutput("id", "-gn", user); err == nil && strings.TrimSpace(g) != "" {
		group = strings.TrimSpace(g)
	}
	if err := run(out, "chown", "-R", user+":"+group, dir); err != nil {
		return err
	}
	return os.Chmod(dir, 0700)
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
	if commandOK("runuser", "--help") {
		return run(out, "runuser", "-u", user, "--", bin, "node", "enroll", "--token", token, "--endpoint", endpoint, "--state-dir", stateDir)
	}
	return run(out, "sudo", "-u", user, bin, "node", "enroll", "--token", token, "--endpoint", endpoint, "--state-dir", stateDir)
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
