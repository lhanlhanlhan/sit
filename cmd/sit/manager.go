package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/managerd"
	"github.com/spf13/cobra"
)

func newManagerCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "manager",
		Short: "Run the SIT Manager (server)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := manager.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config %s: %w", configPath, err)
			}
			srv, err := managerd.NewServer(cfg)
			if err != nil {
				return fmt.Errorf("init manager: %w", err)
			}
			defer srv.Close()

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			fmt.Printf("sit manager: WSS %s | API/MCP %s | store %s\n", cfg.ListenWSS, cfg.ListenAPI, cfg.StorePath)
			if err := srv.Run(ctx); err != nil && err != context.Canceled {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "/etc/sit/manager.yaml", "path to manager config file")
	return cmd
}
