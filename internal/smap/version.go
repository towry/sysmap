package smap

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
)

type FileVersion struct {
	File     string `json:"file"`
	Format   string `json:"format"`
	Reviewed string `json:"reviewed"`
	Advice   string `json:"advice"`
}

var releaseVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var ProgramVersion = "dev"

func SetProgramVersion(version string) {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
	if version != "dev" && !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	ProgramVersion = version
}

func (f FileVersion) guidance() string {
	check := "sysmap check --refs '" + strings.ReplaceAll(f.File, "'", "'\\''") + "'"
	if f.File == "" {
		check = "sysmap check --refs <file>"
	}
	prefix := ""
	if f.Format == "" {
		prefix = "File format is undeclared. Review sysmap spec before declaring it. "
	}
	if f.Reviewed == "" {
		return prefix + "Review version is unknown. Run " + check + " and review sysmap spec before recording the version used."
	}
	if !releaseVersion.MatchString(ProgramVersion) {
		return prefix + "Current program version is not a stable release; upgrade guidance is unknown. Run " + check + " and review sysmap spec."
	}
	if f.Reviewed == ProgramVersion {
		return prefix + "Reviewed with this release. Version metadata does not verify map accuracy; run " + check + "."
	}
	if compareReleases(f.Reviewed, ProgramVersion) > 0 {
		return prefix + "This file was reviewed with a newer Sysmap release. Upgrade Sysmap before reviewing it; do not replace its recorded review version with an older one."
	}
	if f.Reviewed == "v0.1.1" && ProgramVersion == "v0.1.2" {
		return prefix + "v0.1.1 to v0.1.2: the file format and checks did not change. Run " + check + ", review the map against the code, then update the reviewed version."
	}
	return prefix + "Release versions differ; no verified migration checklist is available for this span. Run " + check + " and review sysmap spec before updating the review version."
}

func compareReleases(left, right string) int {
	a, b := releaseVersion.FindStringSubmatch(left), releaseVersion.FindStringSubmatch(right)
	if a == nil || b == nil {
		return 0
	}
	for i := 1; i <= 3; i++ {
		if len(a[i]) != len(b[i]) {
			if len(a[i]) > len(b[i]) {
				return 1
			}
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
		if a[i] < b[i] {
			return -1
		}
	}
	return 0
}

func (m *Model) VersionText() string {
	var out strings.Builder
	fmt.Fprintf(&out, "sysmap version: %s\n", ProgramVersion)
	for _, f := range m.FileVersions {
		format, reviewed := f.Format, f.Reviewed
		if format == "" {
			format = "undeclared"
		}
		if reviewed == "" {
			reviewed = "unknown"
		}
		fmt.Fprintf(&out, "%s: format %s, reviewed with %s\n  %s\n", f.File, format, reviewed, f.guidance())
	}
	return out.String()
}
