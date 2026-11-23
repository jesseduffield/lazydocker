package gui

import (
	"testing"

	"github.com/jesseduffield/gocui"
	"github.com/jesseduffield/lazydocker/pkg/i18n"
)

func TestCurrentMainTabIsLogs(t *testing.T) {
	gui := &Gui{
		Views: Views{
			Main: &gocui.View{
				Tabs:     []string{"Logs", "Stats"},
				TabIndex: 0,
			},
		},
		Tr: &i18n.TranslationSet{
			LogsTitle: "Logs",
		},
	}

	if !gui.currentMainTabIsLogs() {
		t.Fatalf("expected logs tab to be detected")
	}
}

func TestCurrentMainTabIsLogsFalseWhenOtherTabSelected(t *testing.T) {
	gui := &Gui{
		Views: Views{
			Main: &gocui.View{
				Tabs:     []string{"Logs", "Stats"},
				TabIndex: 1,
			},
		},
		Tr: &i18n.TranslationSet{
			LogsTitle: "Logs",
		},
	}

	if gui.currentMainTabIsLogs() {
		t.Fatalf("expected non-logs tab to return false")
	}
}
