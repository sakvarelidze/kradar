package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sakvarelidze/kradar/internal/config"
	"github.com/sakvarelidze/kradar/internal/helm"
)

func TestCheckAllFetchesRepoIndexOncePerRepo(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`entries:
  nginx-ingress:
    - version: "2.0.1"
    - version: "2.1.0"
  argo-cd:
    - version: "9.1.3"
`))
	}))
	defer srv.Close()

	cfg := config.Config{ChartSources: []config.ChartSource{{Name: "x", URL: srv.URL, Charts: []string{"nginx-ingress", "argo-cd"}}}}
	checker := NewChartChecker(2*time.Second, cfg, time.Hour)
	releases := []helm.ReleaseInfo{
		{Name: "r1", Namespace: "ns", ChartName: "nginx-ingress", ChartVersion: "2.0.1", NormalizedChartName: "nginx-ingress"},
		{Name: "r2", Namespace: "ns", ChartName: "argo-cd", ChartVersion: "9.1.3", NormalizedChartName: "argo-cd"},
	}
	results := checker.CheckAll(context.Background(), releases)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected one index fetch, got %d", got)
	}
	if results["ns/r1"].Status != "outdated" {
		t.Fatalf("expected outdated for nginx-ingress, got %s", results["ns/r1"].Status)
	}
	if results["ns/r2"].Status != "up_to_date" {
		t.Fatalf("expected up_to_date for argo-cd, got %s", results["ns/r2"].Status)
	}
}

func TestParseVersionWithBuildMetadata(t *testing.T) {
	v, err := parseVersion("107.0.1+up0.8.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.String() != "107.0.1+up0.8.1" {
		t.Fatalf("unexpected parsed version: %s", v.String())
	}
}

func TestProbeReposReportsConnectivityAndParse(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("entries:\n  cilium:\n    - version: \"1.0.0\"\n"))
	}))
	defer okSrv.Close()

	notFoundSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	}))
	defer notFoundSrv.Close()

	cfg := config.Config{ChartSources: []config.ChartSource{
		{Name: "ok", URL: okSrv.URL, Charts: []string{"cilium"}},
		{Name: "nf", URL: notFoundSrv.URL, Charts: []string{"vault"}},
	}}
	checker := NewChartChecker(2*time.Second, cfg, time.Hour)
	results := checker.ProbeRepos(context.Background())
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	byName := map[string]RepoProbeResult{}
	for _, r := range results {
		byName[r.Name] = r
	}
	if byName["ok"].EntriesCount == 0 || byName["ok"].Error != "" {
		t.Fatalf("expected parsed entries for ok repo, got %#v", byName["ok"])
	}
	if byName["nf"].StatusCode != 404 {
		t.Fatalf("expected 404 for nf repo, got %#v", byName["nf"])
	}
	if !strings.Contains(byName["nf"].Hint, "Nexus repo") {
		t.Fatalf("expected Nexus hint for 404, got %#v", byName["nf"])
	}
}

func TestCheckAllResolvesIngressAliasInIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`entries:
  ingress-nginx:
    - version: "4.10.0"
`))
	}))
	defer srv.Close()

	cfg := config.Config{ChartSources: []config.ChartSource{{Name: "ing", URL: srv.URL, Charts: []string{"nginx-ingress"}}}}
	checker := NewChartChecker(2*time.Second, cfg, time.Hour)
	releases := []helm.ReleaseInfo{{Name: "r1", Namespace: "ns", ChartName: "nginx-ingress", ChartVersion: "4.9.0", NormalizedChartName: "nginx-ingress"}}
	results := checker.CheckAll(context.Background(), releases)
	if results["ns/r1"].Status != "outdated" {
		t.Fatalf("expected outdated, got %#v", results["ns/r1"])
	}
	if results["ns/r1"].IndexChartKeyTried != "ingress-nginx" {
		t.Fatalf("expected index key ingress-nginx, got %q", results["ns/r1"].IndexChartKeyTried)
	}
}

func TestCheckAllReturnsNoUsableVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`entries:
  cilium:
    - version: "1.0.0-rc.1"
`))
	}))
	defer srv.Close()

	cfg := config.Config{IncludePrerelease: false, ChartSources: []config.ChartSource{{Name: "c", URL: srv.URL, Charts: []string{"cilium"}}}}
	checker := NewChartChecker(2*time.Second, cfg, time.Hour)
	releases := []helm.ReleaseInfo{{Name: "r1", Namespace: "ns", ChartName: "cilium", ChartVersion: "1.0.0", NormalizedChartName: "cilium"}}
	results := checker.CheckAll(context.Background(), releases)
	if results["ns/r1"].Reason != "invalid_version" {
		t.Fatalf("expected reason invalid_version, got %#v", results["ns/r1"])
	}
}
