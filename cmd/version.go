package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build version information",
	Run: func(_ *cobra.Command, _ []string) {
		_, _ = fmt.Fprintf(os.Stdout, "kradar version=%s commit=%s date=%s\n", Version, Commit, Date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
