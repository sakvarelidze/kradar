package cmd

import "testing"

func TestWithBuildInfoDefaults_UsesSemverLikeVersionAndShortCommit(t *testing.T) {
	version, commit, date := withBuildInfoDefaults("dev", "none", "unknown", map[string]string{
		"vcs.revision": "abcdef1234567890",
		"vcs.time":     "2026-03-10T01:02:03Z",
		"main.version": "(devel)",
	})

	if version != "0.0.0-dev" {
		t.Fatalf("expected semver-like dev version, got %q", version)
	}
	if commit != "abcdef1" {
		t.Fatalf("expected short commit sha, got %q", commit)
	}
	if date != "2026-03-10T01:02:03Z" {
		t.Fatalf("expected date from vcs.time, got %q", date)
	}
}

func TestSemverLikeVersion_UsesMainModuleSemverVersion(t *testing.T) {
	version := semverLikeVersion(map[string]string{"main.version": "v1.2.3"}, nil)
	if version != "1.2.3" {
		t.Fatalf("expected semver version from main module, got %q", version)
	}
}

func TestSemverLikeVersion_UsesTagWhenMainVersionIsPseudo(t *testing.T) {
	version := semverLikeVersion(map[string]string{
		"main.version": "v0.0.0-20260311131407-8d22d158cb56",
		"vcs.revision": "8d22d158cb56",
		"vcs.modified": "true",
	}, func(_ string) (string, bool) {
		return "1.4.2", true
	})

	if version != "1.4.2+dirty" {
		t.Fatalf("expected release tag version with dirty metadata, got %q", version)
	}
}

func TestSemverLikeVersion_FallsBackToDevWhenTagUnavailable(t *testing.T) {
	version := semverLikeVersion(map[string]string{
		"main.version": "v0.0.0-20260311131407-8d22d158cb56",
	}, func(_ string) (string, bool) {
		return "", false
	})

	if version != "0.0.0-dev" {
		t.Fatalf("expected dev fallback, got %q", version)
	}
}

func TestSemverLikeVersion_TreatsPseudoVersionWithBuildMetadataAsPseudo(t *testing.T) {
	version := semverLikeVersion(map[string]string{
		"main.version": "v0.0.0-20260311131407-8d22d158cb56+dirty",
	}, func(_ string) (string, bool) {
		return "", false
	})

	if version != "0.0.0-dev" {
		t.Fatalf("expected dev fallback for pseudo-version with metadata, got %q", version)
	}
}

func TestWithBuildInfoDefaults_DoesNotOverrideExplicitValues(t *testing.T) {
	version, commit, date := withBuildInfoDefaults("v1.2.3", "deadbeef", "2026-03-10", map[string]string{
		"vcs.revision": "abc123",
		"vcs.time":     "2026-03-10T01:02:03Z",
		"main.version": "v9.9.9",
	})

	if version != "v1.2.3" || commit != "deadbeef" || date != "2026-03-10" {
		t.Fatalf("expected explicit values to be preserved, got version=%q commit=%q date=%q", version, commit, date)
	}
}

func TestShortSHA(t *testing.T) {
	if got := shortSHA("abcdef123456"); got != "abcdef1" {
		t.Fatalf("expected 7-char short sha, got %q", got)
	}
	if got := shortSHA("abc123"); got != "abc123" {
		t.Fatalf("expected short sha unchanged, got %q", got)
	}
}
