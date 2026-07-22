package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ryanmccool/static-mangal/source"
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
