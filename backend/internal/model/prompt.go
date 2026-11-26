package model

import (
	"regexp"
	"strings"
)

// TelegramPrefix starts every prompt the Telegram bridge delivers.
const TelegramPrefix = "[Telegram] "

// inboxFile is a file the Telegram bridge saved, by its absolute path:
// <data>/telegram/<bridge>/inbox/<date>/<batch>/<name>, after the prompt's
// "image: " (or file, video…) label or "are in " (its messages.md note). The
// data folder may hold spaces; the bridge's own names never do.
var inboxFile = regexp.MustCompile(`(: |are in )/.*?/telegram/[a-z0-9][a-z0-9-]*/inbox/\d{4}-\d{2}-\d{2}/[^/\s]+/([^/\s]+)`)

// PromptFileNames is a prompt as the deck keeps and shows it (lastPrompt): a
// Telegram prompt names its files by absolute path so the agent can open
// them, but every screen gets only their names ("image: 02-photo.jpg"), with
// no home folder or user name. Any other prompt is left as it is.
func PromptFileNames(s string) string {
	if !strings.HasPrefix(s, TelegramPrefix) || !strings.Contains(s, "/telegram/") {
		return s
	}
	return inboxFile.ReplaceAllString(s, "$1$2")
}
