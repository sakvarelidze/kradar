package kube

import "strings"

type FriendlyError struct {
	Short  string
	Detail string
	Hint   string
}

func ClassifyKubeError(err error) FriendlyError {
	if err == nil {
		return FriendlyError{}
	}
	detail := strings.TrimSpace(err.Error())
	lower := strings.ToLower(detail)

	friendly := FriendlyError{
		Short:  "Can't connect to Kubernetes cluster",
		Detail: detail,
		Hint:   "Verify kubeconfig context and cluster network access",
	}

	switch {
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "i/o timeout"):
		friendly.Short = "Can't connect to Kubernetes API (timeout)"
		friendly.Hint = "Check VPN connection and network routes"
	case strings.Contains(lower, "no such host"):
		friendly.Short = "Can't resolve Kubernetes API hostname"
		friendly.Hint = "Check DNS resolver settings and VPN connectivity"
	case strings.Contains(lower, "connection refused"):
		friendly.Short = "Kubernetes API refused connection"
		friendly.Hint = "API server may be down, unreachable, or blocked by firewall"
	case strings.Contains(lower, "x509:"):
		friendly.Short = "TLS verification failed"
		friendly.Hint = "Check kubeconfig certificate authority and TLS interception settings"
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401"):
		friendly.Short = "Authentication failed"
		friendly.Hint = "Check kubeconfig credentials; token or certificate may be expired"
	case strings.Contains(lower, "forbidden") || strings.Contains(lower, "403"):
		friendly.Short = "Access denied (RBAC)"
		friendly.Hint = "Your Kubernetes identity lacks required permissions"
	}

	return friendly
}
