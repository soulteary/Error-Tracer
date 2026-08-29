// Package sdk embeds the browser client distributed by Error-Tracer.
package sdk

import _ "embed"

//go:embed error-tracer.js
var browserScript string

// BrowserScript returns the embedded, dependency-free browser client.
func BrowserScript() string {
	return browserScript
}
