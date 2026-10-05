// Package platform embeds configuration defaults, versions, and manifests for AK3S releases.
package platform

import "embed"

// Files makes release defaults, pinned versions, and templates available without a checkout.
//
//go:embed defaults.yaml versions.yaml values/*.yaml manifests/*.yaml
var Files embed.FS
