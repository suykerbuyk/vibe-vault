package sanitize

import "testing"

func TestStripTags_NoTags(t *testing.T) {
	input := "Hello, this is plain text with no XML tags."
	got := StripTags(input)
	if got != input {
		t.Errorf("StripTags(%q) = %q, want unchanged", input, got)
	}
}

func TestStripTags_AllTagTypes(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"local-command-stdout", "<local-command-stdout>output</local-command-stdout>", "output"},
		{"local-command-stderr", "<local-command-stderr>error</local-command-stderr>", "error"},
		{"local-command-caveat", "<local-command-caveat>caveat</local-command-caveat>", "caveat"},
		{"command-output", "<command-output>result</command-output>", "result"},
		{"command-name", "<command-name>ls</command-name>", "ls"},
		{"command-args", "<command-args>-la</command-args>", "-la"},
		{"command-message", "<command-message>msg</command-message>", "msg"},
		{"system-reminder", "<system-reminder>reminder</system-reminder>", "reminder"},
		{"task-id", "<task-id>123</task-id>", "123"},
		{"task-notification", "<task-notification>done</task-notification>", "done"},
		{"persisted-output", "<persisted-output>data</persisted-output>", "data"},
		{"thinking", "<thinking>thought</thinking>", "thought"},
		{"tool-use-id", "<tool-use-id>abc</tool-use-id>", "abc"},
		{"tool", "<tool>hammer</tool>", "hammer"},
		{"skill-name", "<skill-name>commit</skill-name>", "commit"},
		{"plugin-id", "<plugin-id>p1</plugin-id>", "p1"},
		{"vault", "<vault>secret</vault>", "secret"},
		{"self-closing", "<thinking/>text", "text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripTags(tt.input)
			if got != tt.want {
				t.Errorf("StripTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripTags_NonMatchingTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<html>page</html>", "<html>page</html>"},
		{"<div class='x'>content</div>", "<div class='x'>content</div>"},
		{"<custom-tag>data</custom-tag>", "<custom-tag>data</custom-tag>"},
		{"<b>bold</b>", "<b>bold</b>"},
	}
	for _, tt := range tests {
		got := StripTags(tt.input)
		if got != tt.want {
			t.Errorf("StripTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripTags_NestedContent(t *testing.T) {
	input := "<tool>some content with spaces</tool>"
	got := StripTags(input)
	if got != "some content with spaces" {
		t.Errorf("StripTags(%q) = %q, want %q", input, got, "some content with spaces")
	}

	// Multiple tags wrapping content
	input2 := "<thinking>thought</thinking> and <tool>action</tool>"
	got2 := StripTags(input2)
	if got2 != "thought and action" {
		t.Errorf("StripTags(%q) = %q, want %q", input2, got2, "thought and action")
	}
}

func TestStripTags_EmptyAndWhitespace(t *testing.T) {
	if got := StripTags(""); got != "" {
		t.Errorf("StripTags empty = %q, want empty", got)
	}
	if got := StripTags("   "); got != "" {
		t.Errorf("StripTags whitespace = %q, want empty (trimmed)", got)
	}
	if got := StripTags("  <tool></tool>  "); got != "" {
		t.Errorf("StripTags tags+whitespace = %q, want empty", got)
	}
}
