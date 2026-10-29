package designfoundation

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var declaredDesignVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Read existing blockquote metadata before the first document section, not
// incidental version examples or prose elsewhere in the design document.
func designHeaderValue(body string, keys ...string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "##") {
			break
		}
		if !strings.HasPrefix(line, ">") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		colon := strings.IndexAny(line, ":：")
		if colon < 0 {
			continue
		}
		for _, key := range keys {
			if strings.EqualFold(strings.TrimSpace(line[:colon]), key) {
				return strings.Trim(strings.TrimLeft(line[colon:], ":： "), "` ")
			}
		}
	}
	return ""
}

func checkDeclaredSurfaceVersion(root string, idx *ContractIndex, file, reference, kernelVersion string) []Finding {
	var findings []Finding
	add := func(code, detail string) {
		findings = append(findings, Finding{Code: code, Severity: SeverityWarning, Path: file, Detail: "Surface " + reference + ": " + detail})
	}
	unverified := func(detail string) []Finding {
		add("surface_version_unverified", detail+"; version remains unverified. Main should inspect the existing source, not invent a new registry.")
		return findings
	}
	surfaceID, refVersion, refPinned := strings.Cut(reference, "@")
	surface, ok := idx.Surfaces[surfaceID]
	if !ok {
		return unverified("no matching Surface row in " + KernelRel)
	}
	if refPinned && !declaredDesignVersion.MatchString(refVersion) {
		return unverified("reference version is outside the supported vN.N.N format")
	}
	profile := strings.Trim(surface.Profile, "` ")
	if profile == "inline" {
		if kernelVersion == "" {
			return unverified("inline Surface follows Kernel version, which could not be read")
		}
		if refPinned && refVersion != "v"+kernelVersion {
			add("surface_version_mismatch", "inline Surface follows "+KernelRel+"@v"+kernelVersion+", not "+refVersion)
		}
		return findings
	}
	path, tableVersion, tablePinned := strings.Cut(profile, "@")
	if tablePinned && !declaredDesignVersion.MatchString(tableVersion) {
		return unverified("Profile/version in " + KernelRel + " is outside the supported vN.N.N format")
	}
	path = filepath.Clean(path)
	if path == "." || filepath.IsAbs(path) || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return unverified("Profile path is not an explicit project-relative file")
	}
	if !strings.HasPrefix(filepath.ToSlash(path), "docs/") {
		path = filepath.Join("docs", "design", path)
	}
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return unverified("cannot read declared Profile " + path)
	}
	if designHeaderValue(string(data), "ID") != surfaceID {
		return unverified("Profile " + path + " header ID does not match the Surface row")
	}
	actual := designHeaderValue(string(data), "版本", "version")
	if !declaredDesignVersion.MatchString(actual) {
		return unverified("Profile " + path + " header version is missing or outside the supported vN.N.N format")
	}
	if tablePinned && tableVersion != actual {
		add("surface_version_mismatch", KernelRel+" pins "+path+"@"+tableVersion+" but its header declares "+actual)
	}
	if !refPinned {
		return unverified("Profile " + path + " declares " + actual + " but the Derivation does not declare a version")
	}
	if refVersion != actual {
		add("surface_version_mismatch", "Derivation declares "+refVersion+" but "+path+" declares "+actual)
	}
	if inherits := designHeaderValue(string(data), "继承", "inherits"); inherits != "" {
		source, inheritedVersion, pinned := strings.Cut(inherits, "@")
		if (source != "DESIGN.md" && source != KernelRel) || !pinned || !declaredDesignVersion.MatchString(inheritedVersion) || kernelVersion == "" {
			return unverified("Profile " + path + " inheritance cannot be compared to the current Kernel")
		}
		if inheritedVersion != "v"+kernelVersion {
			add("surface_version_mismatch", path+" inherits "+inherits+" but current "+KernelRel+" is v"+kernelVersion)
		}
	}
	return findings
}
