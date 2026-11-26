package telegram

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// asset is one file of a message to download.
type asset struct {
	kind string // image, video, voice, audio, video note, animation, sticker, file
	id   string
	name string // the file name to save under (made safe), extension maybe added later
	size int64  // as Telegram reported it; 0 = unknown
}

// assets lists every file of a message: photos at their largest size,
// documents, video, voice, audio, video notes, animations and stickers (a
// static sticker is an image; an animated or video one is its file plus
// its thumbnail as an image).
func assets(m *Message) []asset {
	var out []asset
	if len(m.Photo) > 0 {
		big := m.Photo[len(m.Photo)-1]
		for _, p := range m.Photo { // largest last, but don't count on it
			if p.Width*p.Height > big.Width*big.Height {
				big = p
			}
		}
		out = append(out, asset{kind: "image", id: big.FileID, name: "photo", size: big.FileSize})
	}
	doc := func(d *Document, kind, fallback string) {
		if d == nil || d.FileID == "" {
			return
		}
		if kind == "file" && strings.HasPrefix(d.MimeType, "image/") {
			kind = "image"
		}
		out = append(out, asset{kind: kind, id: d.FileID, name: safeName(d.FileName, fallback), size: d.FileSize})
	}
	// An animation also comes as a document; take it once, as the animation.
	if m.Animation != nil {
		doc(m.Animation, "animation", "animation")
	} else {
		doc(m.Document, "file", "file")
	}
	doc(m.Video, "video", "video")
	doc(m.Voice, "voice", "voice")
	doc(m.Audio, "audio", "audio")
	doc(m.VideoNote, "video note", "video_note")
	if s := m.Sticker; s != nil && s.FileID != "" {
		switch {
		case s.IsAnimated:
			out = append(out, asset{kind: "sticker", id: s.FileID, name: "sticker.tgs", size: s.FileSize})
		case s.IsVideo:
			out = append(out, asset{kind: "sticker", id: s.FileID, name: "sticker.webm", size: s.FileSize})
		default:
			out = append(out, asset{kind: "image", id: s.FileID, name: "sticker.webp", size: s.FileSize})
		}
		if (s.IsAnimated || s.IsVideo) && s.Thumbnail != nil && s.Thumbnail.FileID != "" {
			out = append(out, asset{kind: "image", id: s.Thumbnail.FileID, name: "sticker_thumbnail", size: s.Thumbnail.FileSize})
		}
	}
	return out
}

// names hands out file names unique within one batch: name.ext, name-2.ext…
type names map[string]bool

func (n names) take(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	got := name
	for i := 2; n[strings.ToLower(got)]; i++ {
		got = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	n[strings.ToLower(got)] = true
	return got
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeName is a file name with nothing but letters, digits, dots, dashes and
// underscores, never hidden, at most 80 bytes.
func safeName(name, fallback string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	base = strings.Trim(unsafeName.ReplaceAllString(base, "_"), "._")
	ext = unsafeName.ReplaceAllString(ext, "")
	if ext == "." || len(ext) > 10 {
		ext = ""
	}
	if base == "" {
		base = fallback
	}
	if len(base) > 70 {
		base = base[:70]
	}
	return base + ext
}

func megabytes(n int64) string {
	return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
}
