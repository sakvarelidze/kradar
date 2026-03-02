package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sakvarelidze/kradar/internal/appctx"
	"github.com/sakvarelidze/kradar/internal/kube"
	"github.com/spf13/cobra"
)

var reposCmd = &cobra.Command{
	Use:   "repos",
	Short: "Repository utilities",
}

var reposTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test connectivity and parsing for configured chart repositories",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := kube.New(opts.Kubeconfig, opts.Context, kube.ClientOptions{QPS: opts.QPS, Burst: opts.Burst, Debug: opts.Debug})
		if err != nil {
			return friendlyKubeError(err)
		}
		probeCtx, probeCancel := kube.TimeoutContext(5 * time.Second)
		defer probeCancel()
		if err := client.Probe(probeCtx); err != nil {
			return friendlyKubeError(err)
		}

		actx, err := appctx.Build(context.Background(), appctx.Options{ConfigFile: opts.ConfigFile, Timeout: opts.Timeout, CacheTTL: opts.CacheTTL})
		if err != nil {
			return err
		}
		checker := actx.ChartChecker
		results := checker.ProbeRepos(cmd.Context())
		if len(results) == 0 {
			_, _ = fmt.Fprintln(os.Stdout, "no chart repositories configured")
			return nil
		}
		_, _ = fmt.Fprintln(os.Stdout, "NAME\tREPO\tINDEX_URL\tHTTP\tPARSED_ENTRIES\tERROR\tHINT")
		for _, r := range results {
			httpStatus := "-"
			if r.StatusCode > 0 {
				httpStatus = fmt.Sprintf("%d", r.StatusCode)
			}
			errMsg := strings.TrimSpace(r.Error)
			if errMsg == "" {
				errMsg = "-"
			}
			hint := strings.TrimSpace(r.Hint)
			if hint == "" {
				hint = "-"
			}
			_, _ = fmt.Fprintf(os.Stdout, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", r.Name, r.URL, r.IndexURL, httpStatus, r.EntriesCount, errMsg, hint)
		}
		return nil
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Print local diagnostic information for checker wiring",
	RunE: func(_ *cobra.Command, _ []string) error {
		client, err := kube.New(opts.Kubeconfig, opts.Context, kube.ClientOptions{QPS: opts.QPS, Burst: opts.Burst, Debug: opts.Debug})
		if err != nil {
			return friendlyKubeError(err)
		}
		probeCtx, probeCancel := kube.TimeoutContext(5 * time.Second)
		defer probeCancel()
		if err := client.Probe(probeCtx); err != nil {
			return friendlyKubeError(err)
		}

		actx, err := appctx.Build(context.Background(), appctx.Options{ConfigFile: opts.ConfigFile, Timeout: opts.Timeout, CacheTTL: opts.CacheTTL})
		if err != nil {
			return err
		}
		checkerEnabled := actx.ChartChecker != nil && actx.ChartChecker.SourceCount() > 0
		mapping := "argo-cd -> (no mapping)"
		if checkerEnabled {
			for _, repo := range actx.Cfg.ChartSources {
				for _, c := range repo.Charts {
					if c == "argo-cd" {
						mapping = fmt.Sprintf("argo-cd -> %s", repo.Name)
						break
					}
				}
			}
		}
		_, _ = fmt.Fprintf(os.Stdout, "config_path: %s\n", actx.ConfigPath)
		_, _ = fmt.Fprintf(os.Stdout, "chart_sources: %d\n", len(actx.Cfg.ChartSources))
		_, _ = fmt.Fprintf(os.Stdout, "cache_dir: %s\n", actx.ChartChecker.CacheDir())
		_, _ = fmt.Fprintf(os.Stdout, "checker_enabled: %t\n", checkerEnabled)
		_, _ = fmt.Fprintf(os.Stdout, "sample_mapping: %s\n", mapping)
		return nil
	},
}

func init() {
	reposCmd.AddCommand(reposTestCmd)
	reposCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(reposCmd)
}
