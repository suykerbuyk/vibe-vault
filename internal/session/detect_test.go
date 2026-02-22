package session

import (
	"testing"

	"github.com/johns/sesscap/internal/config"
)

func TestDetectProject(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/user/work/my-api", "my-api"},
		{"/home/user/personal/sesscap", "sesscap"},
		{"/home/user/obsidian/ObsMeetings", "ObsMeetings"},
		{"", "_unknown"},
		{"/", "_unknown"},
	}

	for _, tt := range tests {
		got := detectProject(tt.cwd)
		if got != tt.want {
			t.Errorf("detectProject(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestDetectDomain(t *testing.T) {
	cfg := config.Config{
		Domains: config.DomainsConfig{
			Work:       "/home/user/work",
			Personal:   "/home/user/personal",
			Opensource: "/home/user/opensource",
		},
	}

	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/user/work/my-api", "work"},
		{"/home/user/personal/sesscap", "personal"},
		{"/home/user/opensource/linux", "opensource"},
		{"/home/user/random/project", "personal"}, // default
		{"", "personal"},
	}

	for _, tt := range tests {
		got := detectDomain(tt.cwd, cfg)
		if got != tt.want {
			t.Errorf("detectDomain(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestTitleFromFirstMessage(t *testing.T) {
	tests := []struct {
		msg  string
		want string
	}{
		{"Implement the login page", "Implement the login page"},
		{"", "Session"},
		{"hi", "Session"},
		{"This is a very long message that exceeds the maximum length allowed for titles and should be truncated to fit within the display", "This is a very long message that exceeds the maximum length allowed for title..."},
		{"First line\nSecond line", "First line"},
	}

	for _, tt := range tests {
		got := TitleFromFirstMessage(tt.msg)
		if got != tt.want {
			t.Errorf("TitleFromFirstMessage(%q) = %q, want %q", tt.msg, got, tt.want)
		}
	}
}
