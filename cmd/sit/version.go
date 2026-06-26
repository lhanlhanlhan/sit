package main

import (
	"github.com/spf13/cobra"
)

// Version is the build version of the sit binary. Overridable via -ldflags.
var Version = "0.1.0"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the sit version",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Printf("sit/%s\n", Version)
		},
	}
}
