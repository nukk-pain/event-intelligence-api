// Package static embeds the published, version-controlled API documents that the
// read API serves verbatim: the OpenAPI 3.x specification, the agent-facing
// llms.txt summary, and the human-facing browser assets. They live here (not
// under internal/api) because //go:embed cannot reference paths outside its own
// package directory, and because they are product artifacts that other tooling
// (docs, deploy checks) may also read.
package static

import _ "embed"

// OpenAPIYAML is the OpenAPI 3.x specification served at /api/v1/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPIYAML string

// LLMsTxt is the agent-facing API summary served at /llms.txt.
//
//go:embed llms.txt
var LLMsTxt string

// IndexHTML is the human-facing landing page served at / when the client
// prefers HTML (Accept: text/html). API clients (curl, agents, JSON consumers)
// still receive the JSON service index via content negotiation in handleRoot.
//
//go:embed index.html
var IndexHTML string

//go:embed index.css
var IndexCSS string

//go:embed theme.css
var ThemeCSS string

//go:embed list.css
var ListCSS string

//go:embed detail.css
var DetailCSS string

//go:embed ui.js
var UIJS string

//go:embed events.js
var EventsJS string

//go:embed detail.js
var DetailJS string

//go:embed app.js
var AppJS string
