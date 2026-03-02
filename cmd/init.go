package cmd

import (
	"fmt"
	"os"

	"github.com/sakvarelidze/kradar/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize or update kradar config",
	RunE: func(_ *cobra.Command, _ []string) error {
		path, err := config.ResolvePath(opts.ConfigFile)
		if err != nil {
			return err
		}
		if err := config.RunInitWizard(path, os.Stdin, os.Stdout); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(os.Stdout, "config written: %s\n", path)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
