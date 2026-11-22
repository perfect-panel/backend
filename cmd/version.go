package cmd

import (
	"fmt"

	"github.com/perfect-panel/backend/internal/app/buildinfo"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "PPanel version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("[PPanel version] " + buildinfo.Display())
	},
}
