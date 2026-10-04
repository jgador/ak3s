// Package platform contains the immutable configuration shipped in an AK3S release.
package platform

import "embed"

//go:embed defaults.yaml versions.yaml values/*.yaml manifests/*.yaml
var Files embed.FS
