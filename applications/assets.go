// Package applications embeds optional application recipes independently of the platform.
package applications

import "embed"

// Files contains the PostgreSQL recipe and its Helm chart.
//
//go:embed catalog.yaml postgres/Chart.yaml postgres/values.yaml postgres/templates/*.yaml
var Files embed.FS
