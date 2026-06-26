package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:           "sit",
		Short:         "SIT (StayInTouch) — lightweight remote management",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			if showVersion {
				printVersion(cmd)
				return
			}
			_ = cmd.Help()
		},
	}
	root.Flags().BoolVar(&showVersion, "version", false, "print the sit version")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newManagerCmd())
	root.AddCommand(newNodeCmd())
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
