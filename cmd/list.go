package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sakvarelidze/kradar/internal/check"
	"github.com/sakvarelidze/kradar/internal/config"
	"github.com/sakvarelidze/kradar/internal/kube"
	"github.com/sakvarelidze/kradar/internal/output"
	"github.com/sakvarelidze/kradar/internal/scan"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List Helm releases as services",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, cancel := kube.TimeoutContext(opts.Timeout)
		defer cancel()

		client, err := kube.New(opts.Kubeconfig, opts.Context, kube.ClientOptions{QPS: opts.QPS, Burst: opts.Burst, Debug: opts.Debug})
		if err != nil {
			return friendlyKubeError(err)
		}
		probeCtx, probeCancel := kube.TimeoutContext(5 * time.Second)
		defer probeCancel()
		if err := client.Probe(probeCtx); err != nil {
			return friendlyKubeError(err)
		}

		cfg, err := config.Load(opts.ConfigFile)
		if err != nil {
			return err
		}
		chartChecker := check.NewChartChecker(opts.Timeout, cfg, opts.CacheTTL)
		scanner := scan.Scanner{KubeClient: client, Checker: chartChecker}
		namespace, allNamespaces := resolveNamespaceScope(cmd)

		rows, err := scanner.Scan(ctx, scan.Options{
			Namespace:     namespace,
			AllNamespaces: allNamespaces,
			CheckHelmRepo: hasCheck("helmrepo"),
			TruncateImage: true,
			Workers:       opts.Workers,
			Debug:         opts.Debug,
		})
		if err != nil {
			return err
		}

		outdated := 0
		for _, row := range rows {
			if row.ChartStatus == "outdated" {
				outdated++
			}
		}

		if err := output.Render(os.Stdout, opts.Output, rows); err != nil {
			return err
		}
		if strings.EqualFold(opts.FailOn, "outdated") && outdated > 0 {
			return fmt.Errorf("found %d outdated releases", outdated)
		}
		return nil
	},
}

func hasCheck(name string) bool {
	for _, c := range opts.Checks {
		if c == name {
			return true
		}
	}
	return false
}
