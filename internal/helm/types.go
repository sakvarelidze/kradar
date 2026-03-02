package helm

import "time"

// ReleaseInfo represents a Helm release surfaced as a service.
type ReleaseInfo struct {
	Name                string   `json:"name"`
	Namespace           string   `json:"namespace"`
	ChartName           string   `json:"chartName"`
	NormalizedChartName string   `json:"normalizedChartName,omitempty"`
	ChartVersion        string   `json:"chartVersion"`
	AppVersion          string   `json:"appVersion,omitempty"`
	Status              string   `json:"status"`
	Updated             string   `json:"updated,omitempty"`
	PodCount            *int     `json:"podCount,omitempty"`
	Images              []string `json:"images,omitempty"`
}

// ServiceRow is the final user-facing result row.
type ServiceRow struct {
	Namespace           string   `json:"namespace"`
	Release             string   `json:"release"`
	Chart               string   `json:"chart"`
	ChartVer            string   `json:"chartVersion"`
	AppVer              string   `json:"appVersion,omitempty"`
	Pods                *int     `json:"pods,omitempty"`
	PodError            string   `json:"podError,omitempty"`
	ChartStatus         string   `json:"chartStatus"`
	ChartStatusReason   string   `json:"chartStatusReason,omitempty"`
	ChartSourceName     string   `json:"chartSourceName,omitempty"`
	ChartSourceURL      string   `json:"chartSourceURL,omitempty"`
	LatestVersion       string   `json:"latestVersion,omitempty"`
	ChartNameRaw        string   `json:"chartNameRaw,omitempty"`
	ChartNameNormalized string   `json:"chartNameNormalized,omitempty"`
	RepoName            string   `json:"repoName,omitempty"`
	RepoURL             string   `json:"repoURL,omitempty"`
	IndexChartKeyTried  string   `json:"indexChartKeyTried,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	FetchError          string   `json:"fetchError,omitempty"`
	Images              []string `json:"images,omitempty"`
	ImagesSummary       string   `json:"imagesSummary,omitempty"`
	Checks              []Check  `json:"checks,omitempty"`
}

type Check struct {
	Type                string `json:"type"`
	Status              string `json:"status"`
	Installed           string `json:"installed,omitempty"`
	Latest              string `json:"latest,omitempty"`
	Reason              string `json:"reason,omitempty"`
	SourceName          string `json:"sourceName,omitempty"`
	SourceURL           string `json:"sourceURL,omitempty"`
	ChartNameRaw        string `json:"chartNameRaw,omitempty"`
	ChartNameNormalized string `json:"chartNameNormalized,omitempty"`
	RepoName            string `json:"repoName,omitempty"`
	RepoURL             string `json:"repoURL,omitempty"`
	IndexChartKeyTried  string `json:"indexChartKeyTried,omitempty"`
	FetchError          string `json:"fetchError,omitempty"`
	Message             string `json:"message,omitempty"`
	CheckedAt           string `json:"checkedAt,omitempty"`
}

func NewChartCheck(status, installed, latest, reason, message string) Check {
	return Check{
		Type:      "chart",
		Status:    status,
		Installed: installed,
		Latest:    latest,
		Reason:    reason,
		Message:   message,
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
