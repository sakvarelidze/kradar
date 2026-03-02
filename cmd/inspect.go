package cmd

import (
	"fmt"
	"os"

	"github.com/sakvarelidze/kradar/internal/check"
	"github.com/sakvarelidze/kradar/internal/config"
	"github.com/sakvarelidze/kradar/internal/helm"
	"github.com/sakvarelidze/kradar/internal/kube"
	"github.com/sakvarelidze/kradar/internal/output"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <release>",
	Short: "Inspect a single Helm release",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		releaseName := args[0]
		ctx, cancel := kube.TimeoutContext(opts.Timeout)
		defer cancel()

		client, err := kube.New(opts.Kubeconfig, opts.Context, kube.ClientOptions{QPS: opts.QPS, Burst: opts.Burst, Debug: opts.Debug})
		if err != nil {
			return err
		}
		releases, err := helm.DiscoverReleases(ctx, client.Clientset, []string{opts.Namespace})
		if err != nil {
			return err
		}

		cfg, err := config.Load(opts.ConfigFile)
		if err != nil {
			return err
		}
		chartChecker := check.NewChartChecker(opts.Timeout, cfg, opts.CacheTTL)

		for _, rel := range releases {
			if rel.Name != releaseName {
				continue
			}
			selector := fmt.Sprintf("app.kubernetes.io/instance=%s", rel.Name)
			podList, err := client.Clientset.CoreV1().Pods(rel.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				return err
			}
			podCount := len(podList.Items)
			imagesSet := map[string]struct{}{}
			for _, pod := range podList.Items {
				for _, img := range kube.ImagesFromPodSpec(pod.Spec) {
					imagesSet[img] = struct{}{}
				}
			}
			images := make([]string, 0, len(imagesSet))
			for img := range imagesSet {
				images = append(images, img)
			}

			chk := chartChecker.Check(ctx, rel)
			row := helm.ServiceRow{
				Namespace:           rel.Namespace,
				Release:             rel.Name,
				Chart:               rel.ChartName,
				ChartVer:            rel.ChartVersion,
				AppVer:              rel.AppVersion,
				Pods:                &podCount,
				ChartStatus:         chk.Status,
				ChartStatusReason:   chk.Reason,
				ChartSourceName:     chk.SourceName,
				ChartSourceURL:      chk.SourceURL,
				LatestVersion:       chk.Latest,
				ChartNameRaw:        chk.ChartNameRaw,
				ChartNameNormalized: chk.ChartNameNormalized,
				RepoName:            chk.RepoName,
				RepoURL:             chk.RepoURL,
				IndexChartKeyTried:  chk.IndexChartKeyTried,
				Reason:              chk.Reason,
				FetchError:          chk.FetchError,
				Images:              images,
				Checks:              []helm.Check{chk},
			}
			return output.Render(os.Stdout, opts.Output, []helm.ServiceRow{row})
		}
		return fmt.Errorf("release %q not found in namespace %q", releaseName, opts.Namespace)
	},
}
