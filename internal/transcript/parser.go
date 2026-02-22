package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseFile reads and parses a Claude Code JSONL transcript file.
func ParseFile(path string) (*Transcript, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads a JSONL transcript from a reader.
func Parse(r io.Reader) (*Transcript, error) {
	var entries []Entry
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var entry Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip unparseable lines rather than failing the whole transcript
			continue
		}

		// Skip non-conversation types
		if entry.Type == "file-history-snapshot" || entry.Type == "progress" {
			continue
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript: %w", err)
	}

	stats := computeStats(entries)
	return &Transcript{Entries: entries, Stats: stats}, nil
}

// ContentBlocks extracts typed content blocks from a message.
// Handles both string content and array content.
func ContentBlocks(msg *Message) []ContentBlock {
	if msg == nil {
		return nil
	}

	switch c := msg.Content.(type) {
	case string:
		return []ContentBlock{{Type: "text", Text: c}}
	case []interface{}:
		var blocks []ContentBlock
		for _, item := range c {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			b, err := json.Marshal(m)
			if err != nil {
				continue
			}
			var block ContentBlock
			if err := json.Unmarshal(b, &block); err != nil {
				continue
			}
			blocks = append(blocks, block)
		}
		return blocks
	}
	return nil
}

// TextContent extracts all text from an assistant message, ignoring thinking blocks.
func TextContent(msg *Message) string {
	blocks := ContentBlocks(msg)
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ToolUses extracts all tool_use blocks from an assistant message.
func ToolUses(msg *Message) []ContentBlock {
	blocks := ContentBlocks(msg)
	var tools []ContentBlock
	for _, b := range blocks {
		if b.Type == "tool_use" {
			tools = append(tools, b)
		}
	}
	return tools
}
