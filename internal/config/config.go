package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type SourceAuth struct {
	Type        string `yaml:"type,omitempty"`
	UsernameEnv string `yaml:"username_env,omitempty"`
	PasswordEnv string `yaml:"password_env,omitempty"`
	TokenEnv    string `yaml:"token_env,omitempty"`
	HeaderName  string `yaml:"header_name,omitempty"`
	ValueEnv    string `yaml:"value_env,omitempty"`
	CertFile    string `yaml:"cert_file,omitempty"`
	KeyFile     string `yaml:"key_file,omitempty"`
}

type SourceTLS struct {
	Insecure bool   `yaml:"insecure,omitempty"`
	CAFile   string `yaml:"ca_file,omitempty"`
}

type SourceNetwork struct {
	Timeout  time.Duration `yaml:"timeout,omitempty"`
	CacheTTL time.Duration `yaml:"cache_ttl,omitempty"`
}

type ChartSource struct {
	Name           string            `yaml:"name"`
	Type           string            `yaml:"type,omitempty"`
	URL            string            `yaml:"url"`
	Charts         []string          `yaml:"charts"`
	Priority       int               `yaml:"priority,omitempty"`
	Auth           SourceAuth        `yaml:"auth,omitempty"`
	TLS            SourceTLS         `yaml:"tls,omitempty"`
	Network        SourceNetwork     `yaml:"network,omitempty"`
	CAFile         string            `yaml:"ca_file,omitempty"`          // legacy
	ClientCertFile string            `yaml:"client_cert_file,omitempty"` // legacy
	ClientKeyFile  string            `yaml:"client_key_file,omitempty"`  // legacy
	BearerTokenEnv string            `yaml:"bearer_token_env,omitempty"` // legacy
	Headers        map[string]string `yaml:"headers,omitempty"`          // legacy
	BasicAuth      *BasicAuth        `yaml:"basic_auth,omitempty"`       // legacy
}

type BasicAuth struct {
	Username    string `yaml:"username"`
	PasswordEnv string `yaml:"password_env"`
}

type Config struct {
	ChartSources      []ChartSource     `yaml:"chart_sources"`
	ChartRepos        []ChartSource     `yaml:"chart_repos,omitempty"`
	IncludePrerelease bool              `yaml:"include_prerelease,omitempty"`
	Github            map[string]string `yaml:"github,omitempty"`
	ImageSources      []map[string]any  `yaml:"image_sources,omitempty"`
}

func Load(path string) (Config, error) {
	cfgPath, err := ResolvePath(path)
	if err != nil {
		return Config{}, err
	}
	if _, err := os.Stat(cfgPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := writeDefaultConfig(cfgPath); err != nil {
				return Config{}, err
			}
		} else {
			return Config{}, err
		}
	}
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{ChartSources: defaultChartSources()}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return Config{}, err
	}
	if len(cfg.ChartSources) == 0 && len(cfg.ChartRepos) > 0 {
		cfg.ChartSources = cfg.ChartRepos
	}
	if len(cfg.ChartSources) == 0 {
		cfg.ChartSources = defaultChartSources()
	}
	cfg.ChartSources = normalizeSources(cfg.ChartSources)
	cfg.ChartRepos = cfg.ChartSources
	return cfg, nil
}

func ResolvePath(path string) (string, error) {
	if strings.TrimSpace(path) != "" {
		return path, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "kradar", "config.yaml"), nil
}

func writeDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	cfg := Config{ChartSources: defaultChartSources(), IncludePrerelease: false, ImageSources: []map[string]any{{"name": "dockerhub", "type": "dockerhub", "enabled": false, "auth": map[string]any{"type": "none"}}}}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func normalizeSources(in []ChartSource) []ChartSource {
	out := make([]ChartSource, 0, len(in))
	for _, s := range in {
		s.Name = strings.TrimSpace(s.Name)
		s.URL = strings.TrimSpace(s.URL)
		if s.Type == "" {
			s.Type = "helm_index"
		}
		if len(s.Charts) == 0 {
			s.Charts = []string{"*"}
		}
		if s.Network.Timeout == 0 {
			s.Network.Timeout = 5 * time.Second
		}
		if s.Network.CacheTTL == 0 {
			s.Network.CacheTTL = 6 * time.Hour
		}
		if s.Auth.Type == "" {
			s.Auth.Type = "none"
		}
		if s.Auth.Type == "none" && s.BearerTokenEnv != "" {
			s.Auth.Type = "bearer_env"
			s.Auth.TokenEnv = s.BearerTokenEnv
		}
		if s.Auth.Type == "none" && s.BasicAuth != nil {
			s.Auth.Type = "basic_env"
			s.Auth.UsernameEnv = s.BasicAuth.Username
			s.Auth.PasswordEnv = s.BasicAuth.PasswordEnv
		}
		if s.TLS.CAFile == "" {
			s.TLS.CAFile = s.CAFile
		}
		if s.Auth.CertFile == "" {
			s.Auth.CertFile = s.ClientCertFile
		}
		if s.Auth.KeyFile == "" {
			s.Auth.KeyFile = s.ClientKeyFile
		}
		if len(s.Headers) > 0 && s.Auth.Type == "none" {
			for k, v := range s.Headers {
				s.Auth.Type = "header_env"
				s.Auth.HeaderName = k
				s.Auth.ValueEnv = v
				break
			}
		}
		out = append(out, s)
	}
	return out
}

func defaultChartSources() []ChartSource {
	return []ChartSource{
		{Name: "bitnami", Type: "helm_index", URL: "https://charts.bitnami.com/bitnami", Charts: []string{"*"}, Priority: 10},
		{Name: "ingress-nginx", Type: "helm_index", URL: "https://kubernetes.github.io/ingress-nginx", Charts: []string{"ingress-nginx", "nginx-ingress"}, Priority: 50},
		{Name: "prometheus-community", Type: "helm_index", URL: "https://prometheus-community.github.io/helm-charts", Charts: []string{"*"}, Priority: 10},
		{Name: "grafana", Type: "helm_index", URL: "https://grafana.github.io/helm-charts", Charts: []string{"*"}, Priority: 10},
		{Name: "argo", Type: "helm_index", URL: "https://argoproj.github.io/argo-helm", Charts: []string{"argo-cd", "argo-workflows", "argocd"}, Priority: 50},
		{Name: "hashicorp", Type: "helm_index", URL: "https://helm.releases.hashicorp.com", Charts: []string{"vault", "consul", "nomad"}, Priority: 50},
		{Name: "jetstack", Type: "helm_index", URL: "https://charts.jetstack.io", Charts: []string{"cert-manager"}, Priority: 50},
		{Name: "cilium", Type: "helm_index", URL: "https://helm.cilium.io", Charts: []string{"cilium"}, Priority: 50},
		{Name: "kyverno", Type: "helm_index", URL: "https://kyverno.github.io/kyverno", Charts: []string{"kyverno"}, Priority: 50},
		{Name: "metallb", Type: "helm_index", URL: "https://metallb.github.io/metallb", Charts: []string{"metallb"}, Priority: 50},
		{Name: "rancher", Type: "helm_index", URL: "https://releases.rancher.com/server-charts/latest", Charts: []string{"rancher", "rancher-webhook"}, Priority: 30},
		{Name: "gitlab", Type: "helm_index", URL: "https://charts.gitlab.io", Charts: []string{"gitlab", "gitlab-runner"}, Priority: 30},
		{Name: "traefik", Type: "helm_index", URL: "https://helm.traefik.io/traefik", Charts: []string{"traefik"}, Priority: 30},
		{Name: "elastic", Type: "helm_index", URL: "https://helm.elastic.co", Charts: []string{"elasticsearch", "kibana", "filebeat", "metricbeat", "apm-server", "logstash"}, Priority: 30},
		{Name: "istio", Type: "helm_index", URL: "https://istio-release.storage.googleapis.com/charts", Charts: []string{"istio-base", "istiod", "gateway", "cni"}, Priority: 30},
	}
}

func Save(path string, cfg Config) error {
	cfg.ChartRepos = cfg.ChartSources
	body, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func RunInitWizard(path string, in *os.File, out *os.File) error {
	cfg, _ := Load(path)
	resolvedPath, err := ResolvePath(path)
	if err != nil {
		return err
	}
	r := bufio.NewReader(in)
	_, _ = fmt.Fprintln(out, "kradar init")
	_, _ = fmt.Fprintln(out, "Configure Nexus Helm source? [y/N]")
	ans, _ := r.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(ans)) != "y" {
		return Save(resolvedPath, cfg)
	}
	ns := ChartSource{Name: "nexus", Type: "helm_index", Charts: []string{"*"}, Priority: 100}
	_, _ = fmt.Fprintln(out, "Nexus Helm base URL:")
	ns.URL, _ = r.ReadString('\n')
	ns.URL = strings.TrimSpace(ns.URL)
	_, _ = fmt.Fprintln(out, "Chart patterns (comma separated, default * ):")
	patterns, _ := r.ReadString('\n')
	patterns = strings.TrimSpace(patterns)
	if patterns != "" {
		ns.Charts = splitCSV(patterns)
	}
	_, _ = fmt.Fprintln(out, "Priority (default 100):")
	prio, _ := r.ReadString('\n')
	prio = strings.TrimSpace(prio)
	if prio != "" {
		if p, e := strconv.Atoi(prio); e == nil {
			ns.Priority = p
		}
	}
	_, _ = fmt.Fprintln(out, "Auth type [none/basic_env/bearer_env/header_env/mtls]:")
	auth, _ := r.ReadString('\n')
	ns.Auth.Type = strings.TrimSpace(auth)
	if ns.Auth.Type == "" {
		ns.Auth.Type = "none"
	}
	switch ns.Auth.Type {
	case "basic_env":
		_, _ = fmt.Fprintln(out, "Username env var:")
		ns.Auth.UsernameEnv, _ = r.ReadString('\n')
		ns.Auth.UsernameEnv = strings.TrimSpace(ns.Auth.UsernameEnv)
		_, _ = fmt.Fprintln(out, "Password env var:")
		ns.Auth.PasswordEnv, _ = r.ReadString('\n')
		ns.Auth.PasswordEnv = strings.TrimSpace(ns.Auth.PasswordEnv)
	case "bearer_env":
		_, _ = fmt.Fprintln(out, "Token env var:")
		ns.Auth.TokenEnv, _ = r.ReadString('\n')
		ns.Auth.TokenEnv = strings.TrimSpace(ns.Auth.TokenEnv)
	case "header_env":
		_, _ = fmt.Fprintln(out, "Header name:")
		ns.Auth.HeaderName, _ = r.ReadString('\n')
		ns.Auth.HeaderName = strings.TrimSpace(ns.Auth.HeaderName)
		_, _ = fmt.Fprintln(out, "Header value env var:")
		ns.Auth.ValueEnv, _ = r.ReadString('\n')
		ns.Auth.ValueEnv = strings.TrimSpace(ns.Auth.ValueEnv)
	case "mtls":
		_, _ = fmt.Fprintln(out, "Client cert file:")
		ns.Auth.CertFile, _ = r.ReadString('\n')
		ns.Auth.CertFile = strings.TrimSpace(ns.Auth.CertFile)
		_, _ = fmt.Fprintln(out, "Client key file:")
		ns.Auth.KeyFile, _ = r.ReadString('\n')
		ns.Auth.KeyFile = strings.TrimSpace(ns.Auth.KeyFile)
	}
	cfg.ChartSources = append(cfg.ChartSources, ns)
	return Save(resolvedPath, cfg)
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{"*"}
	}
	return out
}
