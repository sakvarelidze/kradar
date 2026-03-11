package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"

	goPseudoVersionPattern = regexp.MustCompile(`^v?0\.0\.0-\d{14}-[0-9a-f]{12}(\+.*)?$`)
)

func init() {
	Version, Commit, Date = withBuildInfoDefaults(Version, Commit, Date, readBuildSettings())
	rootCmd.AddCommand(versionCmd)
}

func readBuildSettings() map[string]string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}

	settings := make(map[string]string, len(info.Settings)+1)
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	settings["main.version"] = info.Main.Version

	return settings
}

func withBuildInfoDefaults(version, commit, date string, settings map[string]string) (string, string, string) {
	if settings == nil {
		return version, commit, date
	}

	if version == "dev" {
		version = semverLikeVersion(settings, latestSemverTagForRevision)
	}

	if commit == "none" {
		if vcsRevision := settings["vcs.revision"]; vcsRevision != "" {
			commit = shortSHA(vcsRevision)
		}
	}

	if date == "unknown" {
		if vcsTime := settings["vcs.time"]; vcsTime != "" {
			date = vcsTime
		}
	}

	return version, commit, date
}

func semverLikeVersion(settings map[string]string, tagLookup func(string) (string, bool)) string {
	if mv := strings.TrimSpace(settings["main.version"]); mv != "" && mv != "(devel)" {
		if v, err := semver.NewVersion(strings.TrimPrefix(mv, "v")); err == nil {
			if !isGoPseudoVersion(mv) {
				return withDirtySuffix(v.String(), settings)
			}
		}
	}

	if tagLookup != nil {
		if tag, ok := tagLookup(settings["vcs.revision"]); ok {
			return withDirtySuffix(tag, settings)
		}
	}

	return withDirtySuffix("0.0.0-dev", settings)
}

func latestSemverTagForRevision(revision string) (string, bool) {
	revision = strings.TrimSpace(revision)
	if revision == "" {
		return "", false
	}

	out, err := exec.Command("git", "tag", "--points-at", revision).Output()
	if err != nil {
		return "", false
	}

	lines := strings.Split(string(out), "\n")
	var best *semver.Version
	for _, line := range lines {
		tag := strings.TrimSpace(line)
		if tag == "" {
			continue
		}
		v, err := semver.NewVersion(strings.TrimPrefix(tag, "v"))
		if err != nil {
			continue
		}
		if best == nil || v.GreaterThan(best) {
			best = v
		}
	}

	if best == nil {
		return "", false
	}
	return best.String(), true
}

func isGoPseudoVersion(v string) bool {
	return goPseudoVersionPattern.MatchString(strings.TrimSpace(v))
}

func withDirtySuffix(version string, settings map[string]string) string {
	if settings["vcs.modified"] != "true" || strings.Contains(version, "+") {
		return version
	}
	return version + "+dirty"
}

func shortSHA(revision string) string {
	revision = strings.TrimSpace(revision)
	if len(revision) <= 7 {
		return revision
	}
	return revision[:7]
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build version information",
	Run: func(_ *cobra.Command, _ []string) {
		_, _ = fmt.Fprintf(os.Stdout, "kradar version=%s commit=%s date=%s\n", Version, Commit, Date)
	},
}
