// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/help"
)

// supportedProviders is the set of provider short names accepted by
// `vv config set-key`. Order matches the user-facing error message.
var supportedProviders = []string{"anthropic", "openai", "google"}

// runConfig dispatches the `vv config` subcommand group.
func runConfig() {
	args := os.Args[2:]

	if len(args) > 0 {
		switch args[0] {
		case "set-key":
			if wantsHelp(args[1:]) {
				fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdConfigSetKey))
				return
			}
			configPath := filepath.Join(config.ConfigDir(), "config.toml")
			if err := runConfigSetKey(args[1:], os.Stdin, configPath); err != nil {
				fatal("%v", err)
			}
			return
		}
	}

	// `vv config` with no subcommand or `--help` prints group help.
	fmt.Fprint(os.Stderr, help.FormatTerminal(help.CmdConfig))
}

// runConfigSetKey implements `vv config set-key [--force] <provider> <key|->`.
// stdin is consumed only when the key argument is "-". configPath is the
// target ~/.config/vibe-vault/config.toml (or test-supplied alternative).
//
// Errors are user-facing strings (no "vv: " prefix); the caller wraps them
// with fatal().
func runConfigSetKey(args []string, stdin io.Reader, configPath string) error {
	force := hasFlag(args, "--force")
	args = removeFlag(args, "--force")

	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			return fmt.Errorf("unknown flag: %s", a)
		}
	}

	if len(args) != 2 {
		return fmt.Errorf("usage: vv config set-key [--force] <provider> <key|->")
	}

	provider := args[0]
	if !isSupportedProvider(provider) {
		return fmt.Errorf("unknown provider %q; supported: %s", provider, strings.Join(supportedProviders, ", "))
	}

	keyArg := args[1]
	key, err := readKeyValue(keyArg, stdin)
	if err != nil {
		return err
	}
	if vErr := validateKey(key); vErr != nil {
		return vErr
	}

	existing, exists, err := loadFileIfPresent(configPath)
	if err != nil {
		return err
	}

	if exists {
		if existingKey, ok := readProviderKey(existing, provider); ok && existingKey != "" && !force {
			return fmt.Errorf("key already set for %s; pass --force to overwrite", provider)
		}
	}

	var newContent string
	if !exists {
		newContent = fmt.Sprintf("[providers.%s]\napi_key = %q\n", provider, key)
	} else {
		newContent = setProviderKeyInPlace(existing, provider, key)
	}

	if err := atomicWriteConfig(configPath, []byte(newContent)); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Set %s API key in %s\n", provider, configPath)
	return nil
}

// isSupportedProvider reports whether name is one of the providers accepted
// by `vv config set-key`.
func isSupportedProvider(name string) bool {
	for _, p := range supportedProviders {
		if name == p {
			return true
		}
	}
	return false
}

// readKeyValue resolves the key value: returns keyArg directly unless it is
// "-", in which case stdin is read fully and a single trailing '\n' is
// trimmed (so `echo $KEY | vv config set-key ... -` works).
func readKeyValue(keyArg string, stdin io.Reader) (string, error) {
	if keyArg != "-" {
		return keyArg, nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	s := string(data)
	s = strings.TrimSuffix(s, "\n")
	return s, nil
}

// validateKey rejects empty keys and keys that contain newlines, carriage
// returns, or have leading/trailing whitespace.
func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("invalid key: key cannot be empty")
	}
	if strings.ContainsAny(key, "\n\r") {
		return fmt.Errorf("invalid key: must not contain whitespace")
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("invalid key: must not contain whitespace")
	}
	return nil
}

// loadFileIfPresent returns (data, exists, error). A non-existent file is
// not an error; exists==false signals "treat as fresh".
func loadFileIfPresent(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read config: %w", err)
	}
	return data, true, nil
}

// providerSectionRE matches the TOML section header for a given provider,
// allowing surrounding whitespace and optional trailing inline comment.
func providerSectionRE(provider string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*\[providers\.` + regexp.QuoteMeta(provider) + `\]\s*(?:#.*)?$`)
}

// apiKeyLineRE matches an api_key assignment line. Group 1 captures any
// inline comment trailing the value (including the leading "#"), so callers
// can preserve it when replacing the value.
var apiKeyLineRE = regexp.MustCompile(`(?m)^(\s*)api_key\s*=\s*(?:"[^"]*"|'[^']*')\s*(#[^\n]*)?$`)

// readProviderKey extracts the existing api_key value from the
// [providers.<provider>] section, if any. Returns ("", false) when the
// section is absent or the section has no api_key. Returns the raw inner
// string (no surrounding quotes); empty string is reported as ("", true)
// when the line exists but the value is "".
func readProviderKey(content []byte, provider string) (string, bool) {
	sectionStart, sectionEnd, ok := findProviderSection(content, provider)
	if !ok {
		return "", false
	}
	section := content[sectionStart:sectionEnd]
	loc := apiKeyLineRE.FindSubmatchIndex(section)
	if loc == nil {
		return "", false
	}
	line := string(section[loc[0]:loc[1]])
	val := extractTOMLString(line)
	return val, true
}

// extractTOMLString pulls the quoted value out of an `api_key = "..."` line.
// Returns "" when the line cannot be parsed (caller should treat as absent).
func extractTOMLString(line string) string {
	dq := regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`).FindStringSubmatch(line)
	if dq != nil {
		return dq[1]
	}
	sq := regexp.MustCompile(`'([^']*)'`).FindStringSubmatch(line)
	if sq != nil {
		return sq[1]
	}
	return ""
}

// findProviderSection returns the byte offsets [start, end) of the section
// body for [providers.<provider>], where start is the first byte after the
// section header line's terminating newline and end is the start of the
// next section header (or len(content) if this is the last section).
// Returns ok==false if the section header isn't present.
func findProviderSection(content []byte, provider string) (int, int, bool) {
	headerRE := providerSectionRE(provider)
	headerLoc := headerRE.FindIndex(content)
	if headerLoc == nil {
		return 0, 0, false
	}
	// Body starts after the header line (skip its trailing newline if any).
	bodyStart := headerLoc[1]
	if bodyStart < len(content) && content[bodyStart] == '\n' {
		bodyStart++
	}
	// Body ends at the next section header (any [section]) or EOF.
	nextSectionRE := regexp.MustCompile(`(?m)^\s*\[`)
	rel := nextSectionRE.FindIndex(content[bodyStart:])
	if rel == nil {
		return bodyStart, len(content), true
	}
	return bodyStart, bodyStart + rel[0], true
}

// setProviderKeyInPlace returns the content with the provider's api_key set
// to key, performing minimal line-oriented edits and preserving everything
// else (comments, blank lines, unrelated sections, indentation, inline
// comments after the value).
func setProviderKeyInPlace(content []byte, provider, key string) string {
	headerRE := providerSectionRE(provider)
	headerLoc := headerRE.FindIndex(content)

	if headerLoc == nil {
		// No [providers.<P>] section — append cleanly.
		out := string(content)
		// Ensure exactly one blank line of separation before the new section.
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if !strings.HasSuffix(out, "\n\n") {
			out += "\n"
		}
		out += fmt.Sprintf("[providers.%s]\napi_key = %q\n", provider, key)
		return out
	}

	bodyStart, bodyEnd, _ := findProviderSection(content, provider)
	section := content[bodyStart:bodyEnd]

	loc := apiKeyLineRE.FindSubmatchIndex(section)
	if loc == nil {
		// Section exists but no api_key line — insert one immediately under
		// the header.
		newLine := fmt.Sprintf("api_key = %q\n", key)
		var b strings.Builder
		b.Write(content[:bodyStart])
		b.WriteString(newLine)
		b.Write(content[bodyStart:])
		return b.String()
	}

	// Line exists — replace just the value, preserving indentation and any
	// trailing inline comment.
	indentStart, indentEnd := loc[2], loc[3]
	indent := string(section[indentStart:indentEnd])

	var trailing string
	if loc[4] != -1 {
		trailing = " " + string(section[loc[4]:loc[5]])
	}
	newLine := fmt.Sprintf("%sapi_key = %q%s", indent, key, trailing)

	lineStart, lineEnd := loc[0], loc[1]
	var b strings.Builder
	b.Write(content[:bodyStart])
	b.Write(section[:lineStart])
	b.WriteString(newLine)
	b.Write(section[lineEnd:])
	b.Write(content[bodyEnd:])
	return b.String()
}

// atomicWriteConfig writes data to path via a temp file in the same
// directory + chmod 0600 + rename, ensuring the file never exists in a
// partial state. The parent directory is created if missing and chmod'd to
// 0700 (idempotent, defensive — single-user secret-bearing config).
//
// Pattern-matches internal/wrapbundlecache/cache.go:Write().
func atomicWriteConfig(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".config-setkey-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, writeErr := tmp.Write(data); writeErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", writeErr)
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", closeErr)
	}
	if chmodErr := os.Chmod(tmpPath, 0o600); chmodErr != nil {
		cleanup()
		return fmt.Errorf("chmod temp: %w", chmodErr)
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		cleanup()
		return fmt.Errorf("rename temp into place: %w", renameErr)
	}
	// Defensive: tighten the parent directory perms now that a sensitive
	// file lives there. Idempotent on directories already at 0700.
	if chmodErr := os.Chmod(dir, 0o700); chmodErr != nil {
		return fmt.Errorf("chmod config dir: %w", chmodErr)
	}
	return nil
}
