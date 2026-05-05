package gui

import (
	"fmt"
	"time"
)

// CopyToClipboard copies s to the system clipboard and shows a temporary status message
func (gui *Gui) CopyToClipboard(s string) error {
	if err := gui.OSCommand.CopyToClipboard(s); err != nil {
		return err
	}

	// show a short status message in the appStatus area
	// try to be slightly more informative if possible
	msg := gui.Tr.CopiedFmt
	copiedContent := ""
	if len(s) > 0 {
		// show up to 50 chars of the copied content
		short := s
		if len(short) > 50 {
			short = short[:50] + "..."
		}
		copiedContent = short
	}
	// format using translation (if CopiedFmt is empty fall back)
	if gui.Tr.CopiedFmt != "" {
		if copiedContent != "" {
			msg = fmt.Sprintf(gui.Tr.CopiedFmt, copiedContent)
		} else {
			msg = fmt.Sprintf(gui.Tr.CopiedFmt, "")
		}
	} else {
		if copiedContent != "" {
			msg = "Copied: " + copiedContent
		} else {
			msg = "Copied to clipboard"
		}
	}

	// show the message through statusManager so it doesn't get clobbered by other status updates
	gui.AddMessage(msg, 2*time.Second)

	return nil
}
