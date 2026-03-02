package cmd

import "github.com/spf13/cobra"

func resolveNamespaceScope(cmd *cobra.Command) (string, bool) {
	if cmd.Flags().Changed("namespace") {
		return opts.Namespace, false
	}
	return opts.Namespace, opts.AllNamespaces
}
