package utils

import "regexp"

// CacheBustPathPattern is the allowlist pattern for Akamai cache bust paths.
// It MUST stay identical to the +kubebuilder:validation:items:Pattern marker on
// FrontendSpec.AkamaiCacheBustPaths in api/v1alpha1/frontend_types.go. The
// parity is asserted by TestCacheBustPatternMatchesCRDMarker.
const CacheBustPathPattern = `^[a-zA-Z0-9\-._~:/?#\[\]@!&*+,;=%]+$`

// validCacheBustPathRe is a deliberately strict allowlist for CDN cache purge
// paths. It is a subset of RFC 3986 URL characters: it intentionally omits the
// sub-delims ( ) ' $ along with spaces, quotes, backticks, pipes, newlines and
// backslashes, none of which are expected in a real purge URL. Primary shell
// injection defense is passing paths as argv (see cacheBustPurgeScript); this
// regex is belt-and-suspenders. Keep it in sync with the kubebuilder
// items:Pattern marker on FrontendSpec.AkamaiCacheBustPaths.
var validCacheBustPathRe = regexp.MustCompile(CacheBustPathPattern)

func IsValidCacheBustPath(path string) bool {
	return validCacheBustPathRe.MatchString(path)
}
