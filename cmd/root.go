package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var opts = Options{}

type Options struct {
	Kubeconfig         string
	Context            string
	Namespace          string
	AllNamespaces      bool
	Output             string
	Timeout            time.Duration
	Checks             []string
	CheckImages        bool
	CheckGithub        bool
	CacheTTL           time.Duration
	MaxRequestsPerHost int
	ConfigFile         string
	FailOn             string
	RefreshInterval    time.Duration
	QPS                float32
	Burst              int
	Workers            int
	Debug              bool
}

var rootCmd = &cobra.Command{
	Use:   "kradar",
	Short: "Inspect Helm-installed services in Kubernetes",
	RunE: func(cmd *cobra.Command, args []string) error {
		return tuiCmd.RunE(cmd, args)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&opts.Kubeconfig, "kubeconfig", "", "Path to kubeconfig (defaults to in-cluster)")
	rootCmd.PersistentFlags().StringVar(&opts.Context, "context", "", "Kubeconfig context")
	rootCmd.PersistentFlags().StringVarP(&opts.Namespace, "namespace", "n", "default", "Namespace scope")
	rootCmd.PersistentFlags().BoolVarP(&opts.AllNamespaces, "all-namespaces", "A", true, "Scan all namespaces")
	rootCmd.PersistentFlags().StringVarP(&opts.Output, "output", "o", "table", "Output format: table|json")
	rootCmd.PersistentFlags().DurationVar(&opts.Timeout, "timeout", 30*time.Second, "Command timeout")
	rootCmd.PersistentFlags().StringSliceVar(&opts.Checks, "check", []string{"helmrepo"}, "Checks to run (helmrepo)")
	rootCmd.PersistentFlags().BoolVar(&opts.CheckImages, "check-images", false, "Enable image freshness checks")
	rootCmd.PersistentFlags().BoolVar(&opts.CheckGithub, "check-github", false, "Enable GitHub release checks")
	rootCmd.PersistentFlags().DurationVar(&opts.CacheTTL, "cache-ttl", 6*time.Hour, "HTTP cache TTL")
	rootCmd.PersistentFlags().IntVar(&opts.MaxRequestsPerHost, "max-requests-per-host", 4, "Max requests per host")
	rootCmd.PersistentFlags().StringVar(&opts.ConfigFile, "config", "", "Path to configuration YAML")
	rootCmd.PersistentFlags().StringVar(&opts.FailOn, "fail-on", "", "Set to outdated to return non-zero when outdated releases are found")
	rootCmd.PersistentFlags().DurationVar(&opts.RefreshInterval, "refresh", 15*time.Second, "Refresh interval for TUI mode")
	rootCmd.PersistentFlags().Float32Var(&opts.QPS, "qps", 50, "Kubernetes client QPS")
	rootCmd.PersistentFlags().IntVar(&opts.Burst, "burst", 100, "Kubernetes client burst")
	rootCmd.PersistentFlags().IntVar(&opts.Workers, "workers", 4, "Namespace scan worker count")
	rootCmd.PersistentFlags().BoolVar(&opts.Debug, "debug", false, "Enable verbose logging")

	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(inspectCmd)
	rootCmd.AddCommand(tuiCmd)
}
