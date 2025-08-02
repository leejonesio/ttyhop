// Copyright (c) 2025 Lee Jones
// Licensed under the MIT License. See LICENSE file in the project root for details.

package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestZshScriptOutput(t *testing.T) {
	if zshScript == "" {
		t.Fatal("zshScript constant is empty")
	}

	expectedSubstrings := []string{
		"ttyhop-l()",
		"ttyhop-r()",
		"bindkey '^h' ttyhop-l",
		"bindkey '^l' ttyhop-r",
		"zle",
		"original_h_widget",
		"original_l_widget",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(zshScript, sub) {
			t.Errorf("zshScript is missing expected substring: %q", sub)
		}
	}
}

func TestTmuxTryPaneMove(t *testing.T) {
	// --- Subtest: NoTmux ---
	t.Run("NoTmux", func(t *testing.T) {
		originalTmuxEnv := os.Getenv("TMUX")
		if err := os.Unsetenv("TMUX"); err != nil {
			t.Fatalf("failed to unset TMUX: %v", err)
		}
		defer func() {
			if err := os.Setenv("TMUX", originalTmuxEnv); err != nil {
				t.Fatalf("failed to restore TMUX: %v", err)
			}
		}()

		if tmuxTryPaneMove(true) {
			t.Error("Expected tmuxTryPaneMove to return false when not in a tmux session, but it returned true")
		}
	})

	// --- Subtest: AtEdge ---
	t.Run("AtEdge", func(t *testing.T) {
		if err := os.Setenv("TMUX", "/tmp/tmux-1000/default,21,0"); err != nil {
			t.Fatalf("failed to set TMUX: %v", err)
		}
		defer func() {
			if err := os.Unsetenv("TMUX"); err != nil {
				t.Fatalf("failed to unset TMUX: %v", err)
			}
		}()

		originalRunTmux := runTmuxCmd
		defer func() { runTmuxCmd = originalRunTmux }()
		runTmuxCmd = func(args ...string) (string, error) {
			if strings.Contains(strings.Join(args, " "), "pane_at_right") {
				return "1", nil // "1" means at the edge
			}
			return "%0", nil
		}

		if tmuxTryPaneMove(true) {
			t.Error("Expected tmuxTryPaneMove to return false when at the right edge, but it returned true")
		}
	})

	// --- Subtest: Success ---
	t.Run("Success", func(t *testing.T) {
		if err := os.Setenv("TMUX", "/tmp/tmux-1000/default,21,0"); err != nil {
			t.Fatalf("failed to set TMUX: %v", err)
		}
		defer func() {
			if err := os.Unsetenv("TMUX"); err != nil {
				t.Fatalf("failed to unset TMUX: %v", err)
			}
		}()

		originalRunTmux := runTmuxCmd
		defer func() { runTmuxCmd = originalRunTmux }()

		var paneIDCallCount int
		runTmuxCmd = func(args ...string) (string, error) {
			switch {
			case strings.Contains(strings.Join(args, " "), "pane_at_right"):
				return "0", nil // Not at edge
			case strings.Contains(strings.Join(args, " "), "display -p #{pane_id}"):
				paneIDCallCount++
				if paneIDCallCount == 1 {
					return "%0", nil // Original pane ID
				}
				return "%1", nil // New pane ID
			case strings.Contains(strings.Join(args, " "), "select-pane -R"):
				return "", nil // Successful move command
			}
			return "", nil
		}

		if !tmuxTryPaneMove(true) {
			t.Error("Expected tmuxTryPaneMove to return true on successful pane move, but it returned false")
		}
	})

	// --- Subtest: CommandError ---
	t.Run("CommandError", func(t *testing.T) {
		if err := os.Setenv("TMUX", "/tmp/tmux-1000/default,21,0"); err != nil {
			t.Fatalf("failed to set TMUX: %v", err)
		}
		defer func() {
			if err := os.Unsetenv("TMUX"); err != nil {
				t.Fatalf("failed to unset TMUX: %v", err)
			}
		}()

		originalRunTmux := runTmuxCmd
		defer func() { runTmuxCmd = originalRunTmux }()
		runTmuxCmd = func(args ...string) (string, error) {
			return "", errors.New("tmux command failed")
		}

		if tmuxTryPaneMove(true) {
			t.Error("Expected tmuxTryPaneMove to return false when a tmux command fails, but it returned true")
		}
	})
}

func TestPickActiveClient(t *testing.T) {
	originalRunTmux := runTmuxCmd
	defer func() { runTmuxCmd = originalRunTmux }()

	t.Run("OneClient", func(t *testing.T) {
		runTmuxCmd = func(args ...string) (string, error) {
			return "/dev/ttys001 1 1627840934", nil
		}
		tty, err := pickActiveClient()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tty != "/dev/ttys001" {
			t.Errorf("expected tty '/dev/ttys001', got %q", tty)
		}
	})

	t.Run("MultipleClientsOneActive", func(t *testing.T) {
		runTmuxCmd = func(args ...string) (string, error) {
			return "/dev/ttys000 0 1627840930\n/dev/ttys001 1 1627840934\n/dev/ttys002 0 1627840932", nil
		}
		tty, err := pickActiveClient()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tty != "/dev/ttys001" {
			t.Errorf("expected tty '/dev/ttys001', got %q", tty)
		}
	})

	t.Run("MultipleClientsInactive", func(t *testing.T) {
		runTmuxCmd = func(args ...string) (string, error) {
			return "/dev/ttys000 0 1627840930\n/dev/ttys001 0 1627840934\n/dev/ttys002 0 1627840932", nil
		}
		tty, err := pickActiveClient()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tty != "/dev/ttys001" {
			t.Errorf("expected tty '/dev/ttys001' (most recent), got %q", tty)
		}
	})

	t.Run("NoClients", func(t *testing.T) {
		runTmuxCmd = func(args ...string) (string, error) {
			return "", nil
		}
		tty, err := pickActiveClient()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tty != "" {
			t.Errorf("expected empty tty, got %q", tty)
		}
	})

	t.Run("CommandError", func(t *testing.T) {
		runTmuxCmd = func(args ...string) (string, error) {
			return "", errors.New("tmux command failed")
		}
		_, err := pickActiveClient()
		if err == nil {
			t.Fatal("expected an error, but got nil")
		}
	})
}

func TestTmuxSelectEdgePane(t *testing.T) {
	originalRunTmux := runTmuxCmd
	defer func() { runTmuxCmd = originalRunTmux }()

	// This mock simulates the sequence of calls within a successful run
	mockTmuxSequence := func(selectedPane *string) func(args ...string) (string, error) {
		return func(args ...string) (string, error) {
			cmd := strings.Join(args, " ")
			switch {
			case strings.HasPrefix(cmd, "list-clients"):
				return "/dev/ttys001 1 1627840934", nil
			case strings.HasPrefix(cmd, "display -p -t /dev/ttys001"):
				return "@1", nil
			case strings.HasPrefix(cmd, "list-panes -t @1"):
				// pane %1 is leftmost, %3 is rightmost
				return "%1 1 0\n%2 0 0\n%3 0 1", nil
			case strings.HasPrefix(cmd, "select-pane -t"):
				*selectedPane = args[2]
				return "", nil
			default:
				return "", errors.New("unexpected command: " + cmd)
			}
		}
	}

	t.Run("SelectLeftmostPaneWhenMovingEast", func(t *testing.T) {
		var selectedPane string
		runTmuxCmd = mockTmuxSequence(&selectedPane)
		tmuxSelectEdgePane(true, 50) // east=true
		if selectedPane != "%1" {
			t.Errorf("expected leftmost pane '%%1' to be selected, but got %q", selectedPane)
		}
	})

	t.Run("SelectRightmostPaneWhenMovingWest", func(t *testing.T) {
		var selectedPane string
		runTmuxCmd = mockTmuxSequence(&selectedPane)
		tmuxSelectEdgePane(false, 50) // east=false
		if selectedPane != "%3" {
			t.Errorf("expected rightmost pane '%%3' to be selected, but got %q", selectedPane)
		}
	})

	t.Run("NoActionIfListPanesFails", func(t *testing.T) {
		var selectedPane string
		// Override the base mock for this specific scenario
		runTmuxCmd = func(args ...string) (string, error) {
			if strings.HasPrefix(strings.Join(args, " "), "list-panes") {
				return "", errors.New("list-panes failed")
			}
			// Fallback to the base mock for other calls like list-clients
			return mockTmuxSequence(&selectedPane)(args...)
		}

		tmuxSelectEdgePane(true, 50)
		if selectedPane != "" {
			t.Errorf("expected no pane to be selected, but got %q", selectedPane)
		}
	})
}
