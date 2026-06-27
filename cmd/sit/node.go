package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sit/sit/internal/node"
	"github.com/spf13/cobra"
)

func newNodeCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Run the SIT Node (resident agent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := node.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config %s: %w", configPath, err)
			}
			if len(cfg.Endpoints) == 0 {
				return node.ErrNoEndpoints
			}
			stateDir := cfg.StateDir
			if stateDir == "" {
				stateDir = node.DefaultStateDir()
			}
			id, err := node.LoadOrCreateIdentity(stateDir)
			if err != nil {
				return fmt.Errorf("load identity: %w", err)
			}
			if id.Secret == "" {
				return fmt.Errorf("node not enrolled: run `sit node enroll --token <t> --endpoint <url>` first")
			}

			agent := node.NewAgent(cfg, id, Version, nil)
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			fmt.Printf("sit node %s: connecting to %v\n", id.NodeID, cfg.Endpoints)
			if err := agent.Run(ctx); err != nil && err != context.Canceled {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "/etc/sit/node.yaml", "path to node config file")
	cmd.AddCommand(newNodeEnrollCmd())
	cmd.AddCommand(newNodeSetupCmd())
	return cmd
}

func newNodeEnrollCmd() *cobra.Command {
	var token, endpoint, stateDir string
	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "Bootstrap a new node using a one-time enrollment token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" || endpoint == "" {
				return fmt.Errorf("--token and --endpoint are required")
			}
			if stateDir == "" {
				stateDir = node.DefaultStateDir()
			}
			id, err := node.LoadOrCreateIdentity(stateDir)
			if err != nil {
				return fmt.Errorf("load identity: %w", err)
			}
			assignedID, secret, err := exchangeEnrollToken(endpoint, token, id.NodeID)
			if err != nil {
				return fmt.Errorf("enroll exchange: %w", err)
			}
			id.NodeID = assignedID
			id.Secret = secret
			if err := node.SaveIdentity(stateDir, id); err != nil {
				return fmt.Errorf("save identity: %w", err)
			}
			fmt.Printf("enrolled: node_id=%s (credential stored at %s)\n", assignedID, stateDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "one-time enrollment token")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "manager API base URL, e.g. https://mgr.example:8443")
	cmd.Flags().StringVar(&stateDir, "state-dir", "", "node state directory (default platform-specific)")
	return cmd
}

// exchangeEnrollToken POSTs the one-time token to the manager and returns the
// issued node_id + long-term secret.
func exchangeEnrollToken(apiBase, token, nodeID string) (string, string, error) {
	body, _ := json.Marshal(map[string]string{"enroll_token": token, "node_id": nodeID})
	url := strings.TrimRight(apiBase, "/") + "/api/v1/enroll/exchange"
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var out struct {
		NodeID string `json:"node_id"`
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.NodeID, out.Secret, nil
}
