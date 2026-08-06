//go:build tools

// tools.go declares gqlgen (and other code generators) as build-time
// dependencies so `go mod tidy` keeps them in go.mod.
// See: https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package tools

import (
	_ "github.com/99designs/gqlgen"
)
