package kube

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
)

type Client struct {
	Clientset    *kubernetes.Clientset
	ContextName  string
	APIServerURL string
}

type PodInfo struct {
	Name     string
	Status   string
	Restarts int
}

type ReplicaSetRevision struct {
	Name      string
	Revision  string
	CreatedAt time.Time
	Images    []string
}

type RolloutHistory struct {
	WorkloadKind string
	WorkloadName string
	Current      *ReplicaSetRevision
	Previous     *ReplicaSetRevision
}

type ClientOptions struct {
	QPS   float32
	Burst int
	Debug bool
}

var configureLogsOnce sync.Once

const (
	requestTimeout        = 5 * time.Second
	dialTimeout           = 5 * time.Second
	tlsHandshakeTimeout   = 5 * time.Second
	responseHeaderTimeout = 5 * time.Second
	idleConnTimeout       = 30 * time.Second
	expectContinueTimeout = 1 * time.Second
)

func New(kubeconfig, kubeContext string, opts ClientOptions) (*Client, error) {
	configureLogsOnce.Do(func() {
		if !opts.Debug {
			klog.SetOutput(io.Discard)
		}
	})

	cfg, info, err := buildConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, err
	}
	if opts.QPS > 0 {
		cfg.QPS = opts.QPS
	}
	if opts.Burst > 0 {
		cfg.Burst = opts.Burst
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{Clientset: cs, ContextName: info.ContextName, APIServerURL: info.APIServerURL}, nil
}

type configInfo struct {
	ContextName  string
	APIServerURL string
}

func buildConfig(kubeconfig, kubeContext string) (*rest.Config, configInfo, error) {
	info := configInfo{}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules = &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	}

	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	deferred := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	rawCfg, rawErr := deferred.RawConfig()
	if rawErr == nil {
		contextName := rawCfg.CurrentContext
		if kubeContext != "" {
			contextName = kubeContext
		}
		if contextName != "" {
			if ctxDef, ok := rawCfg.Contexts[contextName]; ok {
				info.ContextName = contextName
				if cluster, ok := rawCfg.Clusters[ctxDef.Cluster]; ok {
					info.APIServerURL = strings.TrimSpace(cluster.Server)
				}
			}
		}
	}

	cfg, err := deferred.ClientConfig()
	if err == nil {
		hardenRESTConfig(cfg)
		if info.APIServerURL == "" {
			info.APIServerURL = strings.TrimSpace(cfg.Host)
		}
		return cfg, info, nil
	}
	if kubeconfig != "" {
		return nil, info, err
	}
	cfg, inClusterErr := rest.InClusterConfig()
	if inClusterErr != nil {
		return nil, info, inClusterErr
	}
	if info.ContextName == "" {
		info.ContextName = "in-cluster"
	}
	if info.APIServerURL == "" {
		info.APIServerURL = strings.TrimSpace(cfg.Host)
	}
	hardenRESTConfig(cfg)
	return cfg, info, nil
}

func hardenRESTConfig(cfg *rest.Config) {
	if cfg == nil {
		return
	}
	cfg.Timeout = requestTimeout
	dialer := &net.Dialer{Timeout: dialTimeout, KeepAlive: idleConnTimeout}
	cfg.Dial = dialer.DialContext

	existingWrap := cfg.WrapTransport
	cfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		if existingWrap != nil {
			rt = existingWrap(rt)
		}
		return ensureTransportTimeouts(rt)
	}
}

func ensureTransportTimeouts(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			clone := base.Clone()
			clone.Proxy = http.ProxyFromEnvironment
			clone.TLSHandshakeTimeout = tlsHandshakeTimeout
			clone.ResponseHeaderTimeout = responseHeaderTimeout
			clone.IdleConnTimeout = idleConnTimeout
			clone.ExpectContinueTimeout = expectContinueTimeout
			clone.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: idleConnTimeout}).DialContext
			return clone
		}
		return rt
	}
	t, ok := rt.(*http.Transport)
	if !ok {
		return rt
	}
	clone := t.Clone()
	if clone.Proxy == nil {
		clone.Proxy = http.ProxyFromEnvironment
	}
	clone.TLSHandshakeTimeout = tlsHandshakeTimeout
	clone.ResponseHeaderTimeout = responseHeaderTimeout
	clone.IdleConnTimeout = idleConnTimeout
	clone.ExpectContinueTimeout = expectContinueTimeout
	clone.DialContext = (&net.Dialer{Timeout: dialTimeout, KeepAlive: idleConnTimeout}).DialContext
	return clone
}

func (c *Client) Probe(ctx context.Context) error {
	return c.Clientset.Discovery().RESTClient().Get().AbsPath("/version").Do(ctx).Error()
}

func (c *Client) ListNamespaces(ctx context.Context) ([]string, error) {
	list, err := c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, item.Name)
	}
	sort.Strings(out)
	return out, nil
}

func (c *Client) ListPodsByRelease(ctx context.Context, namespace, release string) ([]PodInfo, error) {
	selector := fmt.Sprintf("app.kubernetes.io/instance=%s", release)
	pods, err := c.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	infos := make([]PodInfo, 0, len(pods.Items))
	for _, p := range pods.Items {
		restarts := 0
		for _, cs := range p.Status.ContainerStatuses {
			restarts += int(cs.RestartCount)
		}
		infos = append(infos, PodInfo{Name: p.Name, Status: string(p.Status.Phase), Restarts: restarts})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

func (c *Client) GetRolloutHistoryByRelease(ctx context.Context, namespace, release string) (RolloutHistory, error) {
	history := RolloutHistory{}
	selector := fmt.Sprintf("app.kubernetes.io/instance=%s", release)
	deployments, err := c.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return history, err
	}
	if len(deployments.Items) == 0 {
		return history, nil
	}

	primary := primaryDeployment(deployments.Items, release)
	history.WorkloadKind = "Deployment"
	history.WorkloadName = primary.Name

	rsList, err := c.Clientset.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return history, err
	}
	owned := filterReplicaSetsForDeployment(rsList.Items, primary.Name)
	if len(owned) == 0 {
		return history, nil
	}

	current, previous := selectCurrentPreviousReplicaSets(owned)
	history.Current = replicaSetRevision(current)
	if previous != nil {
		history.Previous = replicaSetRevision(*previous)
	}
	return history, nil
}

func selectCurrentPreviousReplicaSets(replicaSets []appsv1.ReplicaSet) (appsv1.ReplicaSet, *appsv1.ReplicaSet) {
	ordered := append([]appsv1.ReplicaSet(nil), replicaSets...)
	sort.SliceStable(ordered, func(i, j int) bool {
		ri, iok := revisionValue(ordered[i])
		rj, jok := revisionValue(ordered[j])
		if iok != jok {
			return iok
		}
		if iok && ri != rj {
			return ri > rj
		}
		ti := ordered[i].CreationTimestamp.Time
		tj := ordered[j].CreationTimestamp.Time
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return activeReplicaCount(ordered[i]) > activeReplicaCount(ordered[j])
	})

	current := ordered[0]
	currRev, hasCurrRev := revisionValue(current)
	if hasCurrRev {
		for i := range ordered {
			if rev, ok := revisionValue(ordered[i]); ok && rev == currRev-1 {
				previous := ordered[i]
				return current, &previous
			}
		}
	}
	if len(ordered) > 1 {
		previous := ordered[1]
		return current, &previous
	}
	return current, nil
}

func primaryDeployment(items []appsv1.Deployment, release string) appsv1.Deployment {
	best := items[0]
	bestScore := deploymentScore(best, release)
	for i := 1; i < len(items); i++ {
		score := deploymentScore(items[i], release)
		if score > bestScore {
			best = items[i]
			bestScore = score
		}
	}
	return best
}

func deploymentScore(d appsv1.Deployment, release string) int {
	score := int(d.Status.AvailableReplicas)*10 + int(d.Status.Replicas)
	if strings.EqualFold(d.Name, release) {
		score += 100000
	}
	return score
}

func filterReplicaSetsForDeployment(replicaSets []appsv1.ReplicaSet, deploymentName string) []appsv1.ReplicaSet {
	owned := make([]appsv1.ReplicaSet, 0)
	for _, rs := range replicaSets {
		for _, owner := range rs.OwnerReferences {
			if owner.Kind == "Deployment" && owner.Name == deploymentName {
				owned = append(owned, rs)
				break
			}
		}
	}
	return owned
}

func revisionValue(rs appsv1.ReplicaSet) (int, bool) {
	rev := strings.TrimSpace(rs.Annotations["deployment.kubernetes.io/revision"])
	if rev == "" {
		return 0, false
	}
	v, err := strconv.Atoi(rev)
	if err != nil {
		return 0, false
	}
	return v, true
}

func activeReplicaCount(rs appsv1.ReplicaSet) int32 {
	if rs.Status.ReadyReplicas > rs.Status.Replicas {
		return rs.Status.ReadyReplicas
	}
	return rs.Status.Replicas
}

func replicaSetRevision(rs appsv1.ReplicaSet) *ReplicaSetRevision {
	images := ImagesFromPodSpec(rs.Spec.Template.Spec)
	return &ReplicaSetRevision{
		Name:      rs.Name,
		Revision:  strings.TrimSpace(rs.Annotations["deployment.kubernetes.io/revision"]),
		CreatedAt: rs.CreationTimestamp.Time,
		Images:    images,
	}
}

func ImagesFromPodSpec(spec corev1.PodSpec) []string {
	images := make([]string, 0, len(spec.Containers)+len(spec.InitContainers))
	for _, c := range spec.Containers {
		images = append(images, normalizeImage(c.Image))
	}
	for _, c := range spec.InitContainers {
		images = append(images, normalizeImage(c.Image))
	}
	return images
}

func normalizeImage(img string) string {
	return strings.TrimSpace(img)
}

func TimeoutContext(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
