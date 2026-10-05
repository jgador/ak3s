// Package platform contains the immutable configuration shipped in an AK3S release.
package platform

import "embed"

// Files makes release defaults, version pins, and templates available without a checkout.
//
//go:embed defaults.yaml versions.yaml values/*.yaml manifests/*.yaml
var Files embed.FS
