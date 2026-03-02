package helm

import "testing"

func TestNormalizeChartName(t *testing.T) {
	tests := []struct {
		raw, version, want string
	}{
		{raw: "bitnami/nginx-ingress", version: "2.0.1", want: "nginx-ingress"},
		{raw: "nginx-ingress-2.0.1", version: "2.0.1", want: "nginx-ingress"},
		{raw: "argo-cd-9.1.2", version: "9.1.3", want: "argo-cd-9.1.2"},
		{raw: "argo-cd", version: "9.1.3", want: "argo-cd"},
	}
	for _, tt := range tests {
		if got := NormalizeChartName(tt.raw, tt.version); got != tt.want {
			t.Fatalf("NormalizeChartName(%q, %q)=%q want %q", tt.raw, tt.version, got, tt.want)
		}
	}
}
