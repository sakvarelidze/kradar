package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/sakvarelidze/kradar/internal/helm"
)

func Render(w io.Writer, format string, rows []helm.ServiceRow) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	case "table", "":
		return renderTable(w, rows)
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

func renderTable(w io.Writer, rows []helm.ServiceRow) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAMESPACE	RELEASE	CHART@VER	APPVER	PODS	CHART_STATUS	IMAGES")
	for _, r := range rows {
		pods := "unknown"
		if r.Pods != nil {
			pods = fmt.Sprintf("%d", *r.Pods)
		}
		images := "-"
		if len(r.Images) > 0 {
			images = strings.Join(shortenImages(r.Images), ",")
		}
		_, _ = fmt.Fprintf(tw, "%s	%s	%s@%s	%s	%s	%s	%s\n",
			r.Namespace,
			r.Release,
			r.Chart,
			r.ChartVer,
			emptyDash(r.AppVer),
			pods,
			r.ChartStatus,
			images,
		)
	}
	return tw.Flush()
}

func shortenImages(images []string) []string {
	if len(images) <= 2 {
		return images
	}
	short := append([]string{}, images[:2]...)
	short = append(short, fmt.Sprintf("+%d more", len(images)-2))
	return short
}

func emptyDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
