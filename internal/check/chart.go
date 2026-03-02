package check

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/sakvarelidze/kradar/internal/config"
	"github.com/sakvarelidze/kradar/internal/helm"
	"gopkg.in/yaml.v3"
)

type ChartChecker struct {
	sources           []config.ChartSource
	reposByURL        map[string]config.ChartSource
	includePrerelease bool
	cacheTTL          time.Duration
	cacheDir          string
	httpTimeout       time.Duration
	mu                sync.Mutex
}

type RepoIndex struct {
	Entries map[string][]struct {
		Version string `yaml:"version"`
	} `yaml:"entries"`
}

type RepoProbeResult struct {
	Name         string
	URL          string
	IndexURL     string
	StatusCode   int
	EntriesCount int
	Hint         string
	Error        string
}

type normalizedRelease struct {
	rawName        string
	normalizedName string
	repoURL        string
}

func NewChartChecker(timeout time.Duration, cfg config.Config, ttl time.Duration) *ChartChecker {
	reposByURL := map[string]config.ChartSource{}
	for _, repo := range cfg.ChartSources {
		repoCopy := repo
		repoURL := strings.TrimSpace(repo.URL)
		reposByURL[repoURL] = repoCopy
	}
	cacheDir := ""
	if base, err := os.UserCacheDir(); err == nil {
		cacheDir = filepath.Join(base, "kradar", "index")
	}
	return &ChartChecker{
		sources:           cfg.ChartSources,
		reposByURL:        reposByURL,
		includePrerelease: cfg.IncludePrerelease,
		cacheTTL:          ttl,
		cacheDir:          cacheDir,
		httpTimeout:       timeout,
	}
}

func (c *ChartChecker) SourceCount() int {
	return len(c.reposByURL)
}

func (c *ChartChecker) CacheDir() string {
	return c.cacheDir
}

func (c *ChartChecker) Check(ctx context.Context, rel helm.ReleaseInfo) helm.Check {
	results := c.CheckAll(ctx, []helm.ReleaseInfo{rel})
	if chk, ok := results[rel.Namespace+"/"+rel.Name]; ok {
		return chk
	}
	return helm.NewChartCheck("unknown", rel.ChartVersion, "", "invalid_version", "release missing from scan result")
}

func (c *ChartChecker) withSource(check helm.Check, rel normalizedRelease) helm.Check {
	check.ChartNameRaw = rel.rawName
	check.ChartNameNormalized = rel.normalizedName
	repo, ok := c.reposByURL[rel.repoURL]
	if !ok {
		check.SourceURL = rel.repoURL
		check.RepoURL = rel.repoURL
		return check
	}
	check.SourceName = repo.Name
	check.SourceURL = repo.URL
	check.RepoName = repo.Name
	check.RepoURL = repo.URL
	return check
}

func (c *ChartChecker) CheckAll(ctx context.Context, releases []helm.ReleaseInfo) map[string]helm.Check {
	results := make(map[string]helm.Check, len(releases))
	if len(releases) == 0 {
		return results
	}

	releaseMeta := map[string]normalizedRelease{}
	reposNeeded := map[string]struct{}{}
	for _, rel := range releases {
		key := rel.Namespace + "/" + rel.Name
		normalized := rel.NormalizedChartName
		if normalized == "" {
			normalized = helm.NormalizeChartName(rel.ChartName, rel.ChartVersion)
		}
		if strings.HasPrefix(strings.TrimSpace(strings.ToLower(rel.ChartName)), "oci://") {
			meta := normalizedRelease{rawName: rel.ChartName, normalizedName: normalized, repoURL: ""}
			releaseMeta[key] = meta
			chk := helm.NewChartCheck("unknown", rel.ChartVersion, "", "oci_not_supported", "OCI chart source is not supported yet")
			results[key] = c.withSource(chk, meta)
			continue
		}
		repoURL := c.resolveRepoURL(normalized)
		meta := normalizedRelease{rawName: rel.ChartName, normalizedName: normalized, repoURL: repoURL}
		releaseMeta[key] = meta
		if repoURL == "" {
			chk := helm.NewChartCheck("unknown", rel.ChartVersion, "", "no_source_mapping", "chart repository not configured")
			results[key] = c.withSource(chk, meta)
			continue
		}
		reposNeeded[repoURL] = struct{}{}
	}

	indexes := map[string]RepoIndex{}
	fetchErr := map[string]error{}
	for repoURL := range reposNeeded {
		idx, err := c.fetchRepoIndex(ctx, repoURL)
		if err != nil {
			fetchErr[repoURL] = err
			continue
		}
		indexes[repoURL] = idx
	}

	for _, rel := range releases {
		key := rel.Namespace + "/" + rel.Name
		if _, exists := results[key]; exists {
			continue
		}
		meta := releaseMeta[key]
		if err := fetchErr[meta.repoURL]; err != nil {
			reason := "index_fetch_failed"
			errLower := strings.ToLower(err.Error())
			switch {
			case strings.Contains(errLower, "x509:"):
				reason = "tls_error"
			case strings.Contains(errLower, "missing_credentials"):
				reason = "missing_credentials"
			case strings.Contains(errLower, "timeout") || strings.Contains(errLower, "deadline exceeded"):
				reason = "timeout"
			case strings.Contains(errLower, "http 401") || strings.Contains(errLower, "http 403"):
				reason = "auth_failed"
			case strings.Contains(errLower, "http 404"):
				reason = "index_not_found"
			}
			chk := helm.NewChartCheck("unknown", rel.ChartVersion, "", reason, fmt.Sprintf("fetch index: %v", err))
			chk.FetchError = err.Error()
			results[key] = c.withSource(chk, meta)
			continue
		}

		idx := indexes[meta.repoURL]
		indexKey, versions := resolveIndexEntry(idx.Entries, meta.normalizedName)
		if len(versions) == 0 {
			chk := helm.NewChartCheck("unknown", rel.ChartVersion, "", "chart_not_in_index", "chart missing in repo index")
			chk.IndexChartKeyTried = meta.normalizedName
			results[key] = c.withSource(chk, meta)
			continue
		}

		latest, ok := latestSemver(versions, c.includePrerelease)
		if !ok {
			chk := helm.NewChartCheck("unknown", rel.ChartVersion, "", "invalid_version", "no semver chart versions found")
			chk.IndexChartKeyTried = indexKey
			results[key] = c.withSource(chk, meta)
			continue
		}

		installed, err := parseVersion(rel.ChartVersion)
		if err != nil {
			chk := helm.NewChartCheck("unknown", rel.ChartVersion, latest, "invalid_version", "installed chart version is not semver")
			chk.IndexChartKeyTried = indexKey
			results[key] = c.withSource(chk, meta)
			continue
		}
		latestV, err := parseVersion(latest)
		if err != nil {
			chk := helm.NewChartCheck("unknown", rel.ChartVersion, latest, "invalid_version", "latest chart version is not semver")
			chk.IndexChartKeyTried = indexKey
			results[key] = c.withSource(chk, meta)
			continue
		}

		chk := helm.NewChartCheck("unknown", rel.ChartVersion, latest, "", "")
		chk.IndexChartKeyTried = indexKey
		if installed.Equal(latestV) {
			chk.Status = "up_to_date"
		} else if installed.LessThan(latestV) {
			chk.Status = "outdated"
		} else {
			chk.Status = "unknown"
			chk.Reason = "invalid_version"
			chk.Message = "installed version is newer than repo"
		}
		results[key] = c.withSource(chk, meta)
	}

	return results
}

func resolveIndexEntry(entries map[string][]struct {
	Version string `yaml:"version"`
}, normalizedName string) (string, []struct {
	Version string `yaml:"version"`
}) {
	candidates := indexKeyCandidates(normalizedName)
	for _, k := range candidates {
		if versions, ok := entries[k]; ok && len(versions) > 0 {
			return k, versions
		}
	}
	return normalizedName, nil
}

func indexKeyCandidates(name string) []string {
	base := strings.TrimSpace(name)
	if base == "" {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(v string, out *[]string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		*out = append(*out, v)
	}
	out := make([]string, 0, 6)
	add(base, &out)
	add(strings.ReplaceAll(base, "_", "-"), &out)
	add(strings.ReplaceAll(base, "-", "_"), &out)
	if strings.Contains(base, "nginx-ingress") {
		add(strings.ReplaceAll(base, "nginx-ingress", "ingress-nginx"), &out)
	}
	if strings.Contains(base, "ingress-nginx") {
		add(strings.ReplaceAll(base, "ingress-nginx", "nginx-ingress"), &out)
	}
	return out
}

func (c *ChartChecker) ProbeRepos(ctx context.Context) []RepoProbeResult {
	out := make([]RepoProbeResult, 0, len(c.reposByURL))
	for _, repo := range c.reposByURL {
		indexURL := strings.TrimSuffix(repo.URL, "/") + "/index.yaml"
		res := RepoProbeResult{Name: repo.Name, URL: repo.URL, IndexURL: indexURL}
		body, statusCode, err := c.fetchIndexBody(ctx, repo)
		res.StatusCode = statusCode
		if err != nil {
			res.Error = err.Error()
			if strings.Contains(strings.ToLower(res.Error), "x509:") {
				res.Hint = "tls_error: configure chart_sources[].tls.ca_file with your corporate CA"
			}
			if statusCode == http.StatusNotFound {
				res.Hint = "This Nexus repo may not serve index.yaml (common for some proxy/group setups); verify repo type and URL."
			}
			out = append(out, res)
			continue
		}
		var idx RepoIndex
		if err := yaml.Unmarshal(body, &idx); err != nil {
			res.Error = "parse index: " + err.Error()
			out = append(out, res)
			continue
		}
		res.EntriesCount = len(idx.Entries)
		out = append(out, res)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (c *ChartChecker) resolveRepoURL(chart string) string {
	best := c.resolveSource(chart)
	if best == nil {
		return ""
	}
	return strings.TrimSpace(best.URL)
}

func (c *ChartChecker) resolveSource(chart string) *config.ChartSource {
	n := normalizeLookupName(chart)
	if n == "" {
		return nil
	}
	type candidate struct {
		src       config.ChartSource
		matchRank int
		order     int
	}
	var best *candidate
	for i, src := range c.sources {
		for _, p := range src.Charts {
			pattern := normalizeLookupName(p)
			match := 0
			switch {
			case pattern == n:
				match = 3
			case pattern == "*":
				match = 1
			case strings.Contains(pattern, "*"):
				if ok, _ := filepath.Match(pattern, n); ok {
					match = 2
				}
			}
			if match == 0 {
				continue
			}
			cand := candidate{src: src, matchRank: match, order: i}
			if best == nil || cand.matchRank > best.matchRank || (cand.matchRank == best.matchRank && cand.src.Priority > best.src.Priority) || (cand.matchRank == best.matchRank && cand.src.Priority == best.src.Priority && cand.order < best.order) {
				best = &cand
			}
		}
	}
	if best == nil {
		return nil
	}
	return &best.src
}

func normalizeLookupName(v string) string {
	return strings.TrimSpace(strings.ToLower(strings.ReplaceAll(v, "_", "-")))
}

func (c *ChartChecker) fetchRepoIndex(ctx context.Context, repoURL string) (RepoIndex, error) {
	indexURL := strings.TrimSuffix(repoURL, "/") + "/index.yaml"
	if body, ok := c.readCachedIndex(indexURL); ok {
		var idx RepoIndex
		if err := yaml.Unmarshal(body, &idx); err == nil {
			return idx, nil
		}
	}
	repo, ok := c.reposByURL[repoURL]
	if !ok {
		repo = config.ChartSource{URL: repoURL, Type: "helm_index"}
	}
	body, _, err := c.fetchIndexBody(ctx, repo)
	if err != nil {
		return RepoIndex{}, err
	}
	var idx RepoIndex
	if err := yaml.Unmarshal(body, &idx); err != nil {
		return RepoIndex{}, err
	}
	c.writeCachedIndex(indexURL, body)
	return idx, nil
}

func (c *ChartChecker) fetchIndexBody(ctx context.Context, repo config.ChartSource) ([]byte, int, error) {
	indexURL := strings.TrimSuffix(repo.URL, "/") + "/index.yaml"
	client, err := c.newClient(repo)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range repo.Headers {
		req.Header.Set(k, v)
	}
	switch repo.Auth.Type {
	case "basic_env":
		username := strings.TrimSpace(os.Getenv(repo.Auth.UsernameEnv))
		password := os.Getenv(repo.Auth.PasswordEnv)
		if username == "" || repo.Auth.PasswordEnv == "" {
			return nil, 0, fmt.Errorf("missing_credentials: basic auth env vars")
		}
		req.SetBasicAuth(username, password)
	case "bearer_env":
		token := strings.TrimSpace(os.Getenv(repo.Auth.TokenEnv))
		if token == "" {
			return nil, 0, fmt.Errorf("missing_credentials: bearer token env var")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	case "header_env":
		value := strings.TrimSpace(os.Getenv(repo.Auth.ValueEnv))
		if strings.TrimSpace(repo.Auth.HeaderName) == "" || value == "" {
			return nil, 0, fmt.Errorf("missing_credentials: header auth env vars")
		}
		req.Header.Set(repo.Auth.HeaderName, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("http %d", resp.StatusCode)
	}
	return body, resp.StatusCode, nil
}

func (c *ChartChecker) newClient(repo config.ChartSource) (*http.Client, error) {
	transport := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{},
	}
	if repo.TLS.CAFile != "" {
		caPEM, err := os.ReadFile(repo.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca_file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("parse ca_file: no certificates found")
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	if repo.TLS.Insecure {
		transport.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec
	}
	if repo.Auth.CertFile != "" || repo.Auth.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(repo.Auth.CertFile, repo.Auth.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load mTLS cert/key: %w", err)
		}
		transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}
	timeout := c.httpTimeout
	if repo.Network.Timeout > 0 {
		timeout = repo.Network.Timeout
	}
	return &http.Client{Timeout: timeout, Transport: transport}, nil
}

func (c *ChartChecker) readCachedIndex(indexURL string) ([]byte, bool) {
	if c.cacheDir == "" {
		return nil, false
	}
	path := c.cachePath(indexURL)
	st, err := os.Stat(path)
	if err != nil || time.Since(st.ModTime()) > c.cacheTTL {
		return nil, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return body, true
}

func (c *ChartChecker) writeCachedIndex(indexURL string, body []byte) {
	if c.cacheDir == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.cacheDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(c.cachePath(indexURL), body, 0o644)
}

func (c *ChartChecker) cachePath(indexURL string) string {
	sum := sha1.Sum([]byte(indexURL))
	return filepath.Join(c.cacheDir, hex.EncodeToString(sum[:])+".yaml")
}

func latestSemver(versions []struct {
	Version string `yaml:"version"`
}, includePrerelease bool) (string, bool) {
	all := make([]*semver.Version, 0, len(versions))
	raw := map[string]string{}
	for _, v := range versions {
		sv, err := parseVersion(v.Version)
		if err != nil {
			continue
		}
		if sv.Prerelease() != "" && !includePrerelease {
			continue
		}
		all = append(all, sv)
		raw[sv.String()] = v.Version
	}
	if len(all) == 0 {
		return "", false
	}
	sort.Slice(all, func(i, j int) bool { return all[i].LessThan(all[j]) })
	latest := all[len(all)-1].String()
	if orig, ok := raw[latest]; ok {
		return orig, true
	}
	return latest, true
}

func parseVersion(v string) (*semver.Version, error) {
	clean := strings.TrimSpace(strings.TrimPrefix(v, "v"))
	return semver.NewVersion(clean)
}
