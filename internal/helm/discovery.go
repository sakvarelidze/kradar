package helm

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func DiscoverReleases(ctx context.Context, cs kubernetes.Interface, namespaces []string) ([]ReleaseInfo, error) {
	results := make([]ReleaseInfo, 0)
	for _, ns := range namespaces {
		releases, err := DiscoverReleasesInNamespace(ctx, cs, ns)
		if err != nil {
			return nil, err
		}
		results = append(results, releases...)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Namespace == results[j].Namespace {
			return results[i].Name < results[j].Name
		}
		return results[i].Namespace < results[j].Namespace
	})
	return dedupe(results), nil
}

func DiscoverReleasesInNamespace(ctx context.Context, cs kubernetes.Interface, namespace string) ([]ReleaseInfo, error) {
	secrets, err := cs.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, err
	}

	results := make([]ReleaseInfo, 0, len(secrets.Items))
	for _, s := range secrets.Items {
		rel, err := decodeReleaseSecret(s.Data["release"])
		if err != nil || rel == nil {
			continue
		}
		results = append(results, mapRelease(rel))
	}
	return dedupe(results), nil
}

func decodeReleaseSecret(raw []byte) (*release.Release, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	return decodeReleaseString(string(raw))
}

func decodeReleaseString(raw string) (*release.Release, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	decoded := []byte(raw)
	for range 2 {
		step, err := base64.StdEncoding.DecodeString(string(decoded))
		if err != nil {
			break
		}
		decoded = step
	}

	if len(decoded) > 3 && bytes.Equal(decoded[:3], []byte{0x1f, 0x8b, 0x08}) {
		zr, err := gzip.NewReader(bytes.NewReader(decoded))
		if err != nil {
			return nil, err
		}
		defer func() { _ = zr.Close() }()
		decoded, err = io.ReadAll(zr)
		if err != nil {
			return nil, err
		}
	}
	var rel release.Release
	if err := json.Unmarshal(decoded, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func mapRelease(rel *release.Release) ReleaseInfo {
	info := ReleaseInfo{
		Name:      rel.Name,
		Namespace: rel.Namespace,
		Status:    string(rel.Info.Status),
	}
	if rel.Chart != nil {
		info.ChartName = rel.Chart.Metadata.Name
		info.ChartVersion = rel.Chart.Metadata.Version
		info.NormalizedChartName = NormalizeChartName(rel.Chart.Metadata.Name, rel.Chart.Metadata.Version)
		info.AppVersion = rel.Chart.Metadata.AppVersion
	}
	if rel.Info != nil {
		info.Updated = rel.Info.LastDeployed.String()
	}
	return info
}

func dedupe(in []ReleaseInfo) []ReleaseInfo {
	seen := map[string]ReleaseInfo{}
	for _, r := range in {
		key := r.Namespace + "/" + r.Name
		if existing, ok := seen[key]; !ok || existing.Updated < r.Updated {
			seen[key] = r
		}
	}
	out := make([]ReleaseInfo, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace == out[j].Namespace {
			return out[i].Name < out[j].Name
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}
