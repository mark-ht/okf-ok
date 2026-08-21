package lint

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var uriSchemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

func explicitLocal(value string) bool {
	value = strings.SplitN(strings.SplitN(value, "#", 2)[0], "?", 2)[0]
	return strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || path.Ext(value) != ""
}

func localDiagnostics(bundle Bundle, ref Reference, options Options) []Diagnostic {
	target := strings.TrimSpace(ref.Target)
	if target == "" || strings.HasPrefix(target, "#") || uriSchemeRE.MatchString(target) {
		return nil
	}
	if strings.HasPrefix(target, "//") {
		return []Diagnostic{diagnostic("OKF102", SeverityError, ref.Location, ref, "network-path references are not supported")}
	}
	local := strings.SplitN(strings.SplitN(target, "#", 2)[0], "?", 2)[0]
	decoded, err := url.PathUnescape(local)
	if err != nil {
		return []Diagnostic{diagnostic("OKF102", SeverityError, ref.Location, ref, "reference contains invalid percent escaping")}
	}
	if strings.ContainsAny(local, "\\\x00") || strings.ContainsAny(decoded, "\\\x00") || strings.Contains(decoded, "/") && strings.Contains(local, "%2f") {
		return []Diagnostic{diagnostic("OKF100", SeverityError, ref.Location, ref, "reference contains an unsafe path separator")}
	}
	if strings.Contains(strings.ToLower(local), "%2f") || strings.Contains(strings.ToLower(local), "%5c") {
		return []Diagnostic{diagnostic("OKF100", SeverityError, ref.Location, ref, "reference contains an encoded path separator")}
	}
	var resolved string
	if strings.HasPrefix(decoded, "/") {
		resolved = path.Clean(strings.TrimPrefix(decoded, "/"))
	} else {
		resolved = path.Clean(path.Join(path.Dir(ref.Origin), decoded))
	}
	if resolved == "." {
		resolved = ""
	}
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return []Diagnostic{diagnostic("OKF100", SeverityError, ref.Location, ref, "reference escapes the bundle root")}
	}
	if traversesSymlink(bundle, resolved) {
		return []Diagnostic{diagnostic("OKF100", SeverityError, ref.Location, ref, "reference traverses a symlinked bundle path")}
	}
	if _, ok := bundle.Files[resolved]; ok {
		return nil
	}
	if _, ok := bundle.Dirs[resolved]; ok {
		return nil
	}
	if ref.Ambiguous && !options.StrictSourcePaths {
		d := diagnostic("OKF103", SeverityInfo, ref.Location, ref, "source resource may be a non-followable scope descriptor; local target was not checked")
		d.Resolved = resolved
		return []Diagnostic{d}
	}
	d := diagnostic("OKF101", SeverityWarning, ref.Location, ref, fmt.Sprintf("local target %q does not exist in the bundle", resolved))
	d.Resolved = resolved
	return []Diagnostic{d}
}

func traversesSymlink(bundle Bundle, resolved string) bool {
	if resolved == "" {
		return false
	}
	current := bundle.Root
	for _, segment := range strings.Split(resolved, "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return false
		} // A missing path is handled as OKF101.
		if info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
