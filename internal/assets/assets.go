// Package assets contains the framework's built-in styles and document templates.
//
// Keeping these resources behind an internal package makes it explicit that their
// file names and contents are implementation details rather than public API.
package assets

import (
	"embed"
	"fmt"
)

// Files contains all built-in CSS and HTML resources.
//
//go:embed *.css *.html
var Files embed.FS

// MustString returns a built-in text resource and panics when the embedded file
// is missing. A missing resource is a build-time programming error.
func MustString(name string) string {
	data, err := Files.ReadFile(name)
	if err != nil {
		panic(fmt.Errorf("读取内置资源 %q: %w", name, err))
	}
	return string(data)
}
