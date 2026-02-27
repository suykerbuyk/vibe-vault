package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/session"
)

// Input is the JSON object Claude Code sends to hooks via stdin.
type Input struct {
	SessionID           string `json:"session_id"`
	TranscriptPath      string `json:"transcript_path"`
	HookEventName       string `json:"hook_event_name"`
	CWD                 string `json:"cwd"`
	Reason              string `json:"reason,omitempty"`
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
	PermissionMode      string `json:"permission_mode,omitempty"`
}

// Handle reads hook input from stdin and processes it.
func Handle(cfg config.Config, event string) error {
	input, err := readStdin()
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return handleInput(input, event, cfg)
}

// handleInput contains all dispatch logic, separated from stdin reading for testability.
func handleInput(input *Input, event string, cfg config.Config) error {
	// Use event override if provided (e.g., --event stop)
	if event != "" {
		input.HookEventName = event
	}

	// Skip context clears
	if input.Reason == "clear" {
		return nil
	}

	switch input.HookEventName {
	case "SessionEnd", "":
		return handleSessionEnd(input, cfg)
	case "Stop":
		return handleStop(input, cfg)
	default:
		return fmt.Errorf("unknown hook event: %s", input.HookEventName)
	}
}

func readStdin() (*Input, error) {
	// Read all stdin with a timeout
	done := make(chan []byte, 1)
	errCh := make(chan error, 1)

	go func() {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			errCh <- err
			return
		}
		done <- data
	}()

	var data []byte
	select {
	case data = <-done:
	case err := <-errCh:
		return nil, err
	case <-time.After(2 * time.Second):
		return nil, fmt.Errorf("stdin read timeout")
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty stdin")
	}

	var input Input
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("parse stdin JSON: %w", err)
	}

	return &input, nil
}

func handleStop(input *Input, cfg config.Config) error {
	if input.TranscriptPath == "" {
		return nil
	}

	if _, err := os.Stat(input.TranscriptPath); os.IsNotExist(err) {
		return nil
	}

	result, err := session.Capture(session.CaptureOpts{
		TranscriptPath: input.TranscriptPath,
		CWD:            input.CWD,
		SessionID:      input.SessionID,
		Checkpoint:     true,
		SkipEnrichment: true,
	}, cfg)
	if err != nil {
		return fmt.Errorf("capture checkpoint: %w", err)
	}

	if result.Skipped {
		return nil
	}

	fmt.Fprintf(os.Stderr, "vv: checkpoint %s → %s\n", result.Project, result.NotePath)
	return nil
}

func handleSessionEnd(input *Input, cfg config.Config) error {
	if input.TranscriptPath == "" {
		return fmt.Errorf("no transcript_path in hook input")
	}

	result, err := session.Capture(session.CaptureOpts{
		TranscriptPath: input.TranscriptPath,
		CWD:            input.CWD,
		SessionID:      input.SessionID,
	}, cfg)
	if err != nil {
		return fmt.Errorf("capture session: %w", err)
	}

	if result.Skipped {
		fmt.Fprintf(os.Stderr, "vv: skipped (%s)\n", result.Reason)
		return nil
	}

	fmt.Fprintf(os.Stderr, "vv: %s → %s\n", result.Project, result.NotePath)
	return nil
}
