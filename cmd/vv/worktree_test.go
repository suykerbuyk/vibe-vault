// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"reflect"
	"testing"
)

func TestWorktreeGC_CandidateParents_BasicCSV(t *testing.T) {
	got := parseCandidateParentsCSV("main,feat/foo")
	want := []string{"main", "feat/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestWorktreeGC_CandidateParents_TrimsWhitespace(t *testing.T) {
	got := parseCandidateParentsCSV(" main , feat/foo ")
	want := []string{"main", "feat/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestWorktreeGC_CandidateParents_DropsEmpty(t *testing.T) {
	got := parseCandidateParentsCSV("main,,feat/foo,")
	want := []string{"main", "feat/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

// Dedupe is case-sensitive — git refnames distinguish "main" from
// "MAIN", so {"main", "MAIN"} stays as two entries.
func TestWorktreeGC_CandidateParents_Dedupe(t *testing.T) {
	got := parseCandidateParentsCSV("main,feat/foo,main")
	want := []string{"main", "feat/foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}

	gotCase := parseCandidateParentsCSV("main,MAIN")
	wantCase := []string{"main", "MAIN"}
	if !reflect.DeepEqual(gotCase, wantCase) {
		t.Errorf("case-sensitive: got %#v, want %#v", gotCase, wantCase)
	}
}

// All-empty input returns nil so worktreegc.Run invokes the default-
// branch resolver instead of seeing an empty []string{}.
func TestWorktreeGC_CandidateParents_AllEmpty_FallsBackToDefault(t *testing.T) {
	got := parseCandidateParentsCSV(",,,")
	if got != nil {
		t.Errorf("got %#v, want nil", got)
	}

	gotEmpty := parseCandidateParentsCSV("")
	if gotEmpty != nil {
		t.Errorf("empty string: got %#v, want nil", gotEmpty)
	}
}
