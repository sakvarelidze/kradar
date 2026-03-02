package appctx

import (
	"context"
	"net/http"
	"time"

	"github.com/sakvarelidze/kradar/internal/cache"
	"github.com/sakvarelidze/kradar/internal/check"
	"github.com/sakvarelidze/kradar/internal/config"
)

type Options struct {
	ConfigFile string
	Timeout    time.Duration
	CacheTTL   time.Duration
}

type AppCtx struct {
	ConfigPath   string
	Cfg          config.Config
	HTTPClient   *http.Client
	Cache        *cache.HTTPCache
	ChartChecker *check.ChartChecker
}

func Build(_ context.Context, opts Options) (*AppCtx, error) {
	cfgPath, err := config.ResolvePath(opts.ConfigFile)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(opts.ConfigFile)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: opts.Timeout, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}
	c := cache.NewHTTPCache(opts.Timeout, opts.CacheTTL)
	checker := check.NewChartChecker(opts.Timeout, cfg, opts.CacheTTL)
	return &AppCtx{ConfigPath: cfgPath, Cfg: cfg, HTTPClient: httpClient, Cache: c, ChartChecker: checker}, nil
}
