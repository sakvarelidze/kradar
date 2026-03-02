package scan

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/sakvarelidze/kradar/internal/check"
	"github.com/sakvarelidze/kradar/internal/helm"
	"github.com/sakvarelidze/kradar/internal/kube"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Options struct {
	Namespace     string
	AllNamespaces bool
	CheckHelmRepo bool
	TruncateImage bool
	Workers       int
	Debug         bool
	Progress      func(done, total int)
}

type Scanner struct {
	KubeClient *kube.Client
	Checker    *check.ChartChecker
}

type namespaceResult struct {
	releases []helm.ReleaseInfo
	podCount map[string]int
	images   map[string][]string
	err      error
}

func (s *Scanner) Scan(ctx context.Context, opts Options) ([]helm.ServiceRow, error) {
	namespaces := []string{opts.Namespace}
	if opts.AllNamespaces {
		var err error
		namespaces, err = s.KubeClient.ListNamespaces(ctx)
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = 4
	}
	if workers > len(namespaces) && len(namespaces) > 0 {
		workers = len(namespaces)
	}

	jobs := make(chan string)
	results := make(chan namespaceResult, len(namespaces))
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ns := range jobs {
				rels, podCount, images, err := s.scanNamespace(ctx, ns)
				results <- namespaceResult{releases: rels, podCount: podCount, images: images, err: err}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, ns := range namespaces {
			jobs <- ns
		}
	}()

	allRows := make([]helm.ServiceRow, 0)
	allReleases := make([]helm.ReleaseInfo, 0)
	done := 0
	for range namespaces {
		result := <-results
		done++
		if opts.Progress != nil {
			opts.Progress(done, len(namespaces))
		}
		if result.err != nil {
			wg.Wait()
			return nil, result.err
		}
		for _, rel := range result.releases {
			allReleases = append(allReleases, rel)
			count := result.podCount[rel.Name]
			pods := count
			images := result.images[rel.Name]
			row := helm.ServiceRow{
				Namespace:           rel.Namespace,
				Release:             rel.Name,
				Chart:               rel.ChartName,
				ChartVer:            rel.ChartVersion,
				AppVer:              rel.AppVersion,
				Pods:                &pods,
				ChartNameRaw:        rel.ChartName,
				ChartNameNormalized: rel.NormalizedChartName,
				Images:              images,
				ImagesSummary:       summarizeImages(images),
			}

			row.ChartStatus = "unknown"
			if opts.TruncateImage {
				row.ImagesSummary = summarizeImages(truncateImages(row.Images))
			}
			allRows = append(allRows, row)
		}
	}

	wg.Wait()
	if opts.CheckHelmRepo && s.Checker != nil {
		checksByRelease := s.Checker.CheckAll(ctx, allReleases)
		for i := range allRows {
			key := allRows[i].Namespace + "/" + allRows[i].Release
			chk, ok := checksByRelease[key]
			if !ok {
				continue
			}
			allRows[i].Checks = append(allRows[i].Checks, chk)
			if chk.Status == "" {
				allRows[i].ChartStatus = "unknown"
			} else {
				allRows[i].ChartStatus = chk.Status
			}
			allRows[i].ChartSourceName = chk.SourceName
			allRows[i].ChartSourceURL = chk.SourceURL
			allRows[i].LatestVersion = chk.Latest
			allRows[i].RepoName = chk.RepoName
			allRows[i].RepoURL = chk.RepoURL
			allRows[i].IndexChartKeyTried = chk.IndexChartKeyTried
			allRows[i].FetchError = chk.FetchError
			allRows[i].Reason = chk.Reason
			if chk.ChartNameRaw != "" {
				allRows[i].ChartNameRaw = chk.ChartNameRaw
			}
			if chk.ChartNameNormalized != "" {
				allRows[i].ChartNameNormalized = chk.ChartNameNormalized
			}
			if allRows[i].ChartStatus == "unknown" {
				allRows[i].ChartStatusReason = chk.Reason
			}
		}
	}
	sort.Slice(allRows, func(i, j int) bool {
		if allRows[i].Namespace == allRows[j].Namespace {
			return allRows[i].Release < allRows[j].Release
		}
		return allRows[i].Namespace < allRows[j].Namespace
	})
	return allRows, nil
}

func (s *Scanner) scanNamespace(ctx context.Context, namespace string) ([]helm.ReleaseInfo, map[string]int, map[string][]string, error) {
	releases, err := helm.DiscoverReleasesInNamespace(ctx, s.KubeClient.Clientset, namespace)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list pods in namespace %s: %w", namespace, err)
	}

	pods, err := s.KubeClient.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("discover releases in namespace %s: %w", namespace, err)
	}

	podCount := map[string]int{}
	imagesByRelease := map[string][]string{}
	for _, pod := range pods.Items {
		release := pod.Labels["app.kubernetes.io/instance"]
		if release == "" {
			continue
		}
		podCount[release]++
		imagesByRelease[release] = append(imagesByRelease[release], kube.ImagesFromPodSpec(pod.Spec)...)
	}

	imageList := map[string][]string{}
	for _, rel := range releases {
		images, err := s.ExtractImagesForRelease(ctx, namespace, rel.Name, podCount[rel.Name], imagesByRelease[rel.Name])
		if err != nil {
			return nil, nil, nil, err
		}
		imageList[rel.Name] = images
		if _, ok := podCount[rel.Name]; !ok {
			podCount[rel.Name] = 0
		}
	}
	return releases, podCount, imageList, nil
}

func (s *Scanner) ExtractImagesForRelease(ctx context.Context, namespace, releaseName string, podCount int, podImages []string) ([]string, error) {
	if podCount > 0 {
		return stableDedupe(podImages), nil
	}
	selector := fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName)
	images := make([]string, 0)

	deps, err := s.KubeClient.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list deployments for release %s/%s: %w", namespace, releaseName, err)
	}
	for _, d := range deps.Items {
		images = append(images, kube.ImagesFromPodSpec(d.Spec.Template.Spec)...)
	}
	sts, err := s.KubeClient.Clientset.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list statefulsets for release %s/%s: %w", namespace, releaseName, err)
	}
	for _, ss := range sts.Items {
		images = append(images, kube.ImagesFromPodSpec(ss.Spec.Template.Spec)...)
	}
	dss, err := s.KubeClient.Clientset.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list daemonsets for release %s/%s: %w", namespace, releaseName, err)
	}
	for _, ds := range dss.Items {
		images = append(images, kube.ImagesFromPodSpec(ds.Spec.Template.Spec)...)
	}
	jobs, err := s.KubeClient.Clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err == nil {
		for _, j := range jobs.Items {
			images = append(images, kube.ImagesFromPodSpec(j.Spec.Template.Spec)...)
		}
	}
	cronJobs, err := s.KubeClient.Clientset.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err == nil {
		for _, cj := range cronJobs.Items {
			images = append(images, kube.ImagesFromPodSpec(cj.Spec.JobTemplate.Spec.Template.Spec)...)
		}
	}
	return stableDedupe(images), nil
}

func stableDedupe(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, img := range in {
		img = normalizeImageRef(img)
		if img == "" {
			continue
		}
		if _, ok := seen[img]; ok {
			continue
		}
		seen[img] = struct{}{}
		out = append(out, img)
	}
	sort.Strings(out)
	return out
}

func normalizeImageRef(img string) string {
	img = strings.TrimSpace(img)
	if img == "" {
		return ""
	}
	return img
}

func truncateImages(images []string) []string {
	if len(images) <= 2 {
		return images
	}
	return append(images[:2], fmt.Sprintf("+%d more", len(images)-2))
}

func summarizeImages(images []string) string {
	if len(images) == 0 {
		return "-"
	}
	if len(images) <= 2 {
		return strings.Join(images, ", ")
	}
	return fmt.Sprintf("%s, %s, +%d more", images[0], images[1], len(images)-2)
}
