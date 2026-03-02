package helm

import (
	"regexp"
	"strings"
)

var semverSuffixRegex = regexp.MustCompile(`^(?P<name>.+)-v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

func NormalizeChartName(raw, chartVersion string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	if strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		name = strings.TrimSpace(parts[len(parts)-1])
	}
	if name == "" {
		return ""
	}

	version := strings.TrimSpace(chartVersion)
	if version == "" {
		return name
	}
	version = strings.TrimPrefix(version, "v")

	matches := semverSuffixRegex.FindStringSubmatch(name)
	if len(matches) == 0 {
		return name
	}
	suffixIdx := semverSuffixRegex.SubexpIndex("name")
	if suffixIdx <= 0 || suffixIdx >= len(matches) {
		return name
	}
	base := matches[suffixIdx]
	suffix := strings.TrimPrefix(strings.TrimPrefix(name, base), "-")
	suffix = strings.TrimPrefix(suffix, "v")
	if suffix == version {
		return base
	}
	return name
}
