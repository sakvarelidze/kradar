package cmd

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sakvarelidze/kradar/internal/appctx"
	"github.com/sakvarelidze/kradar/internal/kube"
	"github.com/sakvarelidze/kradar/internal/scan"
	kradartui "github.com/sakvarelidze/kradar/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch interactive kradar dashboard",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := kube.New(opts.Kubeconfig, opts.Context, kube.ClientOptions{QPS: opts.QPS, Burst: opts.Burst, Debug: opts.Debug})
		if err != nil {
			return err
		}
		actx, err := appctx.Build(context.Background(), appctx.Options{ConfigFile: opts.ConfigFile, Timeout: opts.Timeout, CacheTTL: opts.CacheTTL})
		if err != nil {
			return err
		}
		checker := actx.ChartChecker
		checkerEnabled := checker != nil && checker.SourceCount() > 0
		scanner := &scan.Scanner{KubeClient: client, Checker: checker}
		namespace, allNamespaces := resolveNamespaceScope(cmd)
		model := kradartui.New(scanner, client, scan.Options{
			Namespace:     namespace,
			AllNamespaces: allNamespaces,
			CheckHelmRepo: checkerEnabled,
			TruncateImage: true,
			Workers:       opts.Workers,
			Debug:         opts.Debug,
		}, opts.RefreshInterval, kradartui.Meta{ConfigPath: actx.ConfigPath, CheckerEnabled: checkerEnabled})
		_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
		return err
	},
}
