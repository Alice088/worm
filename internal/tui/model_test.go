package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Alice088/worm/internal/config"
	"github.com/Alice088/worm/internal/session"
	"github.com/Alice088/worm/internal/tui/pages"
)

func newTestModel(t *testing.T) Model {
	t.Helper()

	cfgPath := filepath.Join(t.TempDir(), config.FileName)
	if err := (config.Config{}).WithName("tester").SaveTo(cfgPath); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	m := newModel("test", "worm", cfgPath)
	m.main, _, _, _ = m.main.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.call, _, _ = m.call.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

// press sends a single key press through the model.
func press(m tea.Model, key string) tea.Model {
	var msg tea.KeyPressMsg
	switch key {
	case "enter":
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		msg = tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
	next, _ := m.Update(msg)
	return next
}

func TestMainScreenCreateStartsCall(t *testing.T) {
	m := newTestModel(t)

	// "1" is the Create shortcut.
	next := press(m, "1").(Model)
	if next.screen != ScreenCall {
		t.Fatalf("expected call screen, got %v", next.screen)
	}
	if next.call.RoomName == "" {
		t.Fatal("expected a room name")
	}
	if next.call.Connect == "" {
		t.Fatal("expected connect-data on the call screen")
	}

	cd, err := session.Decode(next.call.Connect)
	if err != nil {
		t.Fatalf("connect-data is not decodable: %v", err)
	}
	if cd.Room != next.call.RoomName {
		t.Fatalf("connect-data room %q != call room %q", cd.Room, next.call.RoomName)
	}
	if cd.Name != "tester" {
		t.Fatalf("unexpected peer name %q", cd.Name)
	}
}

func TestJoinFlowTypesBlob(t *testing.T) {
	blob, _ := session.Encode(session.ConnectData{Room: "room-xyz", Name: "host"})

	m := newTestModel(t)
	m.main = pages.NewMain("worm", "test", "tester", m.cfg.Path())
	m.main, _, _, _ = m.main.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Select "Join a call" (second entry) with the down arrow, then enter.
	down := tea.KeyPressMsg{Code: tea.KeyDown}
	next, _ := m.Update(down)
	m = next.(Model)

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	if m.main.Mode() != pages.ModeJoin {
		t.Fatalf("expected join mode, got %v", m.main.Mode())
	}

	// Type the blob character by character (as a paste would arrive).
	for _, r := range blob {
		next, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if m.screen != ScreenCall {
		t.Fatalf("expected call screen after join, got %v", m.screen)
	}
	if m.call.RoomName != "room-xyz" {
		t.Fatalf("expected joined room room-xyz, got %q", m.call.RoomName)
	}
}

func TestJoinRejectsBadInput(t *testing.T) {
	m := newTestModel(t)
	m.main, _, _, _ = m.main.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	for _, r := range "deadbeef" {
		next, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if m.screen != ScreenMain {
		t.Fatalf("expected to stay on main screen, got %v", m.screen)
	}
	if m.main.Err() == "" {
		t.Fatal("expected a validation error to be shown")
	}
}

func TestMuteToggleAndLeave(t *testing.T) {
	m := press(newTestModel(t), "1").(Model)
	if m.call.Muted {
		t.Fatal("should start unmuted")
	}

	next, _ := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m = next.(Model)
	if !m.call.Muted {
		t.Fatal("expected mute after pressing m")
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	m = next.(Model)
	if m.screen != ScreenMain {
		t.Fatalf("expected main screen after leave, got %v", m.screen)
	}
}

func TestCallScreenShowsMembersControlsAndConnectData(t *testing.T) {
	m := press(newTestModel(t), "1").(Model)

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"PARTICIPANTS", "CONNECT-DATA", "tester", "Leave", "Mute mic"} {
		if !strings.Contains(view, want) {
			t.Fatalf("call screen missing %q:\n%s", want, view)
		}
	}
}

func TestMainScreenShowsBothActions(t *testing.T) {
	view := ansi.Strip(newTestModel(t).View().Content)
	for _, want := range []string{"Create a call", "Join a call"} {
		if !strings.Contains(view, want) {
			t.Fatalf("main screen missing %q:\n%s", want, view)
		}
	}
}

func TestRenamePersistsToConfig(t *testing.T) {
	m := newTestModel(t)

	// Open the rename form with the "3" shortcut.
	next, _ := m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = next.(Model)
	if m.main.Mode() != pages.ModeName {
		t.Fatalf("expected name mode, got %v", m.main.Mode())
	}

	// Clear the pre-filled value, then type a new name.
	for range []rune(m.main.UserName) {
		next, _ = m.updateMain(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = next.(Model)
	}
	for _, r := range "neo" {
		next, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = next.(Model)
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if m.selfName != "neo" {
		t.Fatalf("expected selfName neo, got %q", m.selfName)
	}

	cfg, err := config.LoadFrom(m.cfg.Path())
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Name != "neo" {
		t.Fatalf("expected persisted name neo, got %q", cfg.Name)
	}

	// A brand new model from the same file picks the name up.
	if got := newModel("test", "worm", m.cfg.Path()); got.selfName != "neo" {
		t.Fatalf("expected restored name neo, got %q", got.selfName)
	}
}

func TestRenameNameIsUsedInConnectData(t *testing.T) {
	m := newTestModel(t)
	m = m.rename("neo")

	next := press(m, "1").(Model)
	cd, err := session.Decode(next.call.Connect)
	if err != nil {
		t.Fatalf("decode connect-data: %v", err)
	}
	if cd.Name != "neo" {
		t.Fatalf("expected peer name neo, got %q", cd.Name)
	}
}

func TestRenameRejectsEmptyName(t *testing.T) {
	m := newTestModel(t)

	next, _ := m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m = next.(Model)
	for range []rune(m.main.UserName) {
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = next.(Model)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)

	if m.main.Err() == "" {
		t.Fatal("expected a validation error for an empty name")
	}
	if m.selfName != "tester" {
		t.Fatalf("expected name to stay tester, got %q", m.selfName)
	}
}
