// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
)

// NewRenderWrapTextTool returns a deprecated stub. The renderer
// retired in surface v15 (DESIGN #103). The handler returns an
// actionable error directing the operator to update wrap.md via
// `vv context sync`. Kept registered for one binary release as a
// migration affordance for consumer projects whose wrap.md still
// calls this tool.
func NewRenderWrapTextTool() Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_render_wrap_text",
			Description: "[RETIRED in v15] Run `vv context sync` to update your project's wrap.md. The orchestrator now writes iter narratives inline. See DESIGN #103.",
			InputSchema: json.RawMessage(`{"type": "object"}`),
		},
		Handler: func(_ json.RawMessage) (string, error) {
			return "", fmt.Errorf(
				"vv_render_wrap_text was retired in MCP surface v15 (DESIGN #103); " +
					"the orchestrator now writes iter narratives inline; " +
					"run `vv context sync` in this project to refresh wrap.md, then re-run /wrap")
		},
	}
}
