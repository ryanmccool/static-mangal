package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	key2 "github.com/ryanmccool/static-mangal/key"
	"github.com/ryanmccool/static-mangal/source"
	"github.com/spf13/viper"
)

func TestEnterSelectsCurrentChapterForDownload(t *testing.T) {
	bubble := newBubble()
	bubble.setState(chaptersState)

	chapter := &source.Chapter{Name: "Chapter 1"}
	item := &listItem{internal: chapter}
	if err := bubble.chaptersC.SetItems([]list.Item{item}); err != nil {
		t.Fatal(err)
	}

	bubble.updateChapters(tea.KeyMsg{Type: tea.KeyEnter})

	if bubble.state != confirmState {
		t.Fatalf("state = %v, want confirmation", bubble.state)
	}
	if !item.marked {
		t.Fatal("current chapter was not marked")
	}
	if _, selected := bubble.selectedChapters[chapter]; !selected {
		t.Fatal("current chapter was not selected")
	}
}

func TestConfirmFormatSelection(t *testing.T) {
	previous := viper.GetString(key2.FormatsUse)
	defer viper.Set(key2.FormatsUse, previous)
	viper.Set(key2.FormatsUse, "pdf")

	bubble := newBubble()
	bubble.setState(confirmState)
	if bubble.exportFormat != "pdf" {
		t.Fatalf("initial format = %q, want pdf", bubble.exportFormat)
	}

	bubble.updateConfirm(tea.KeyMsg{Type: tea.KeyRight})
	if bubble.exportFormat == "pdf" {
		t.Fatal("right did not change the export format")
	}

	chapter := &source.Chapter{Name: "Chapter 1"}
	bubble.selectedChapters[chapter] = struct{}{}
	bubble.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	if got := viper.GetString(key2.FormatsUse); got != bubble.exportFormat {
		t.Fatalf("configured format = %q, want %q", got, bubble.exportFormat)
	}
}
