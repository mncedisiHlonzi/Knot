package appinfo

import (
	"regexp"
	"testing"
)

func TestNameIsNotEmpty(t *testing.T) {
	if Name == "" {
		t.Fatal("appinfo.Name must not be empty")
	}
}

func TestVersionIsSemantic(t *testing.T) {
	semver := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	if !semver.MatchString(Version) {
		t.Fatalf("appinfo.Version %q is not a semantic version", Version)
	}
}
