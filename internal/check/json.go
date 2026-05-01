// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import "strings"

// JSONReport is the stable schema emitted by `vv check --json`. Bump
// JSONReport.Version when adding/removing fields; additive changes in
// nested structs (e.g., a new optional field on JSONCheck) do not.
type JSONReport struct {
	Version  int            `json:"version"`
	Binary   JSONBinaryInfo `json:"binary"`
	Checks   []JSONCheck    `json:"checks"`
	Summary  JSONSummary    `json:"summary"`
	ExitCode int            `json:"exit_code"`
}

// JSONBinaryInfo records build-time and surface-version metadata for
// the binary running the check. Populated by the caller (cmd/vv/main.go)
// since `internal/check` does not depend on `internal/mcp` or `internal/surface`
// for these specific values.
type JSONBinaryInfo struct {
	Surface int    `json:"surface"`
	Schema  int    `json:"schema"`
	Tools   int    `json:"tools"`
	Commit  string `json:"commit"`
}

// JSONCheck is the JSON projection of a single Result. Status is the
// lowercased status string ("pass" / "warn" / "fail") regardless of
// what Status.String() returns internally.
type JSONCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// JSONSummary tallies the report by status.
type JSONSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

// ToJSON converts the Report to the v1 JSON schema. The caller supplies
// the binary metadata block since those values come from outside the
// check package (mcp server, surface package, help.Version).
//
// ExitCode is set to 1 when any check failed, 0 otherwise. Callers
// honoring the spec's optional "vault unreachable = 2" exit code can
// override it on the returned struct before marshaling.
func (r Report) ToJSON(binary JSONBinaryInfo) JSONReport {
	out := JSONReport{Version: 1, Binary: binary}
	for _, res := range r.Results {
		out.Checks = append(out.Checks, JSONCheck{
			Name:   res.Name,
			Status: strings.ToLower(res.Status.String()),
			Detail: res.Detail,
		})
		switch res.Status {
		case Pass:
			out.Summary.Pass++
		case Warn:
			out.Summary.Warn++
		case Fail:
			out.Summary.Fail++
		}
	}
	if out.Summary.Fail > 0 {
		out.ExitCode = 1
	}
	return out
}
