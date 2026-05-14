// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package driftfixture is a tiny real Go file used by verify_test.go to
// exercise path:line and path:Symbol ref resolution against on-disk content.
package driftfixture

// Greeting is a package-level const used to exercise the const-decl
// branch of the symbol heuristic.
const Greeting = "hello"

// Counter is a package-level var with an explicit type, exercising the
// "Sym Type" branch of the symbol heuristic.
var Counter int

// Widget is a type declaration the symbol heuristic should find.
type Widget struct {
	Name string
}

// Build is a plain function declaration.
func Build() *Widget {
	return &Widget{Name: "default"}
}

// Render is a method with a receiver, exercising the `func (recv) Sym(`
// form of both the symbol regex and the path:line decl heuristic.
func (w *Widget) Render() string {
	// This comment line is intentionally NOT a declaration so a
	// path:line ref pointing here produces a weak-match warning.
	return w.Name
}
