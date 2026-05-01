// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReportToJSON_Empty(t *testing.T) {
	r := Report{}
	got := r.ToJSON(JSONBinaryInfo{Surface: 11, Schema: 10, Tools: 40, Commit: "abc1234"})
	if got.Version != 1 {
		t.Fatalf("version: got %d, want 1", got.Version)
	}
	if len(got.Checks) != 0 {
		t.Fatalf("checks: got %d, want 0", len(got.Checks))
	}
	if got.ExitCode != 0 {
		t.Fatalf("exit_code: got %d, want 0", got.ExitCode)
	}
	if got.Summary.Pass != 0 || got.Summary.Warn != 0 || got.Summary.Fail != 0 {
		t.Fatalf("summary: got %+v, want zeros", got.Summary)
	}
	if got.Binary.Surface != 11 {
		t.Fatalf("binary.surface: got %d, want 11", got.Binary.Surface)
	}
}

func TestReportToJSON_AllPass(t *testing.T) {
	r := Report{Results: []Result{
		{Name: "a", Status: Pass, Detail: "ok"},
		{Name: "b", Status: Pass, Detail: "fine"},
	}}
	got := r.ToJSON(JSONBinaryInfo{})
	if got.ExitCode != 0 {
		t.Fatalf("exit_code: got %d, want 0", got.ExitCode)
	}
	if got.Summary.Pass != 2 {
		t.Fatalf("summary.pass: got %d, want 2", got.Summary.Pass)
	}
	if got.Summary.Fail != 0 {
		t.Fatalf("summary.fail: got %d, want 0", got.Summary.Fail)
	}
	if len(got.Checks) != 2 {
		t.Fatalf("checks: got %d, want 2", len(got.Checks))
	}
}

func TestReportToJSON_HasFailure(t *testing.T) {
	r := Report{Results: []Result{
		{Name: "a", Status: Pass, Detail: "ok"},
		{Name: "b", Status: Warn, Detail: "meh"},
		{Name: "c", Status: Fail, Detail: "broken"},
	}}
	got := r.ToJSON(JSONBinaryInfo{})
	if got.ExitCode != 1 {
		t.Fatalf("exit_code: got %d, want 1", got.ExitCode)
	}
	if got.Summary.Fail != 1 {
		t.Fatalf("summary.fail: got %d, want 1", got.Summary.Fail)
	}
	if got.Summary.Pass != 1 {
		t.Fatalf("summary.pass: got %d, want 1", got.Summary.Pass)
	}
	if got.Summary.Warn != 1 {
		t.Fatalf("summary.warn: got %d, want 1", got.Summary.Warn)
	}
}

func TestReportToJSON_StatusLowercased(t *testing.T) {
	r := Report{Results: []Result{
		{Name: "a", Status: Pass},
		{Name: "b", Status: Warn},
		{Name: "c", Status: Fail},
	}}
	got := r.ToJSON(JSONBinaryInfo{})
	want := []string{"pass", "warn", "fail"}
	for i, c := range got.Checks {
		if c.Status != want[i] {
			t.Fatalf("checks[%d].status: got %q, want %q", i, c.Status, want[i])
		}
		// And confirm there's no uppercase whatsoever (regression on Status.String()
		// returning "FAIL").
		if c.Status != strings.ToLower(c.Status) {
			t.Fatalf("checks[%d].status not lowercased: %q", i, c.Status)
		}
	}
}

func TestReportToJSON_RoundtripsViaJSON(t *testing.T) {
	// Belt-and-suspenders: marshal the JSONReport through encoding/json and
	// assert key field names match the documented schema.
	r := Report{Results: []Result{{Name: "vault", Status: Pass, Detail: "ok"}}}
	jr := r.ToJSON(JSONBinaryInfo{Surface: 11, Schema: 10, Tools: 40, Commit: "ff20177"})
	b, err := json.Marshal(jr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"version":1`,
		`"binary":`,
		`"surface":11`,
		`"checks":`,
		`"summary":`,
		`"exit_code":0`,
		`"name":"vault"`,
		`"status":"pass"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("marshaled JSON missing %q:\n%s", want, s)
		}
	}
}
