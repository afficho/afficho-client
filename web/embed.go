package web

import "embed"

// FS embeds all static assets and templates for the admin UI.
//
//go:embed static templates
var FS embed.FS
