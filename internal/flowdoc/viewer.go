// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// viewerTemplate is the self-contained HTML+CSS+JS viewer shipped alongside
// every rendered FlowDoc. Render substitutes the placeholder
// `{{.FlowsJSON}}` with the JSON-encoded FlowDoc.
//
//go:embed viewer.html
var viewerTemplate string

// placeholder is the single substitution site in viewer.html. It deliberately
// avoids text/template so we can hand-tune the script-escape rules without
// fighting template-context heuristics.
const placeholder = "{{.FlowsJSON}}"

// Render writes a self-contained HTML viewer for doc to w. The FlowDoc is
// JSON-encoded and inlined into a `<script>const FLOWS = ...;</script>` block
// in the embedded viewer.html template. Any occurrence of `</` in the JSON
// (most commonly inside a description containing `</script>`) is escaped to
// `<\/` so it cannot terminate the surrounding script element prematurely.
//
// doc may be nil, in which case an empty FlowDoc is emitted and the viewer
// renders its "No flows in this document." empty state.
func Render(w io.Writer, doc *FlowDoc) error {
	if w == nil {
		return fmt.Errorf("flowdoc: Render: writer is nil")
	}
	payload := doc
	if payload == nil {
		payload = &FlowDoc{}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("flowdoc: Render: marshal: %w", err)
	}

	// Escape any `</` sequences so a flow description containing `</script>`
	// (or `</style>`, `</body>`, ...) cannot break out of the inline script.
	safe := strings.ReplaceAll(string(raw), "</", "<\\/")

	if !strings.Contains(viewerTemplate, placeholder) {
		return fmt.Errorf("flowdoc: Render: viewer template is missing the %s placeholder", placeholder)
	}
	out := strings.Replace(viewerTemplate, placeholder, safe, 1)

	if _, err := io.WriteString(w, out); err != nil {
		return fmt.Errorf("flowdoc: Render: write: %w", err)
	}
	return nil
}
