// Package telegram is the Telegram bridge: messages one Telegram user sends
// or forwards to a bot become one prompt for one agent on the deck. It talks
// to the Bot API over plain HTTPS (long polling, no webhook), so nothing
// listens on the network for it.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultBase is the Bot API's address.
const DefaultBase = "https://api.telegram.org"

// MaxDownload is the biggest file the Bot API lets a bot download.
const MaxDownload = 20 << 20

// API is a small Bot API client. The token is part of every URL, so every
// error it returns has the token taken out (Go's own errors quote the URL).
type API struct {
	Base  string // DefaultBase, or a test server
	Token string
	HTTP  *http.Client
}

// ErrUnauthorized: the Bot API refused the token (revoked or mistyped).
var ErrUnauthorized = errors.New("the bot token was refused: make a new one with @BotFather and run agentctl telegram setup")

// ErrConflict: another program polls this bot, or it has a webhook.
var ErrConflict = errors.New("another program is reading this bot's messages (or it has a webhook): stop it, or make a separate bot")

func (a *API) base() string {
	if a.Base == "" {
		return DefaultBase
	}
	return strings.TrimRight(a.Base, "/")
}

func (a *API) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return http.DefaultClient
}

// scrub takes the token out of an error's text.
func (a *API) scrub(err error) error {
	if err == nil || a.Token == "" {
		return err
	}
	if s := err.Error(); strings.Contains(s, a.Token) {
		return errors.New(strings.ReplaceAll(s, a.Token, "<token>"))
	}
	return err
}

type reply struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
}

// call posts a Bot API method with form values and decodes its result.
func (a *API) call(ctx context.Context, method string, v url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base()+"/bot"+a.Token+"/"+method, strings.NewReader(v.Encode()))
	if err != nil {
		return a.scrub(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := a.client().Do(req)
	if err != nil {
		return a.scrub(err)
	}
	defer resp.Body.Close()
	var r reply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&r); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d, unreadable answer", method, resp.StatusCode)
	}
	if !r.OK {
		switch r.ErrorCode {
		case http.StatusUnauthorized, http.StatusNotFound: // a wrong token answers 404 too
			return ErrUnauthorized
		case http.StatusConflict:
			return ErrConflict
		}
		return a.scrub(fmt.Errorf("telegram %s: %d %s", method, r.ErrorCode, r.Description))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(r.Result, out)
}

// GetMe returns the bot itself: it checks the token.
func (a *API) GetMe(ctx context.Context) (User, error) {
	var u User
	err := a.call(ctx, "getMe", url.Values{}, &u)
	return u, err
}

// GetUpdates waits up to timeout for new messages after offset.
func (a *API) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout+20*time.Second)
	defer cancel()
	v := url.Values{
		"offset":          {strconv.FormatInt(offset, 10)},
		"timeout":         {strconv.Itoa(int(timeout / time.Second))},
		"allowed_updates": {`["message"]`},
	}
	var ups []Update
	err := a.call(ctx, "getUpdates", v, &ups)
	return ups, err
}

// SendMessage sends plain text to a chat.
func (a *API) SendMessage(ctx context.Context, chat int64, text string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return a.call(ctx, "sendMessage", url.Values{"chat_id": {strconv.FormatInt(chat, 10)}, "text": {text}}, nil)
}

// Download saves a file the bot received to dst (0600), at most MaxDownload
// bytes. It returns the path the Bot API gave the file (for its extension).
func (a *API) Download(ctx context.Context, fileID, dst string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var f struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if err := a.call(ctx, "getFile", url.Values{"file_id": {fileID}}, &f); err != nil {
		return "", err
	}
	if f.Path == "" {
		return "", errors.New("too big for a bot to download (over 20 MB)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.base()+"/file/bot"+a.Token+"/"+f.Path, nil)
	if err != nil {
		return "", a.scrub(err)
	}
	resp, err := a.client().Do(req)
	if err != nil {
		return "", a.scrub(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(out, io.LimitReader(resp.Body, MaxDownload+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > MaxDownload {
		err = errors.New("over 20 MB")
	}
	if err != nil {
		os.Remove(dst)
		return "", a.scrub(err)
	}
	return f.Path, nil
}

// Update is one getUpdates entry; only messages are asked for.
type Update struct {
	ID      int64    `json:"update_id"`
	Message *Message `json:"message"`
}

// Message is the part of a Telegram message the bridge reads.
type Message struct {
	ID           int64       `json:"message_id"`
	From         *User       `json:"from"`
	Chat         Chat        `json:"chat"`
	Date         int64       `json:"date"`
	Text         string      `json:"text"`
	Caption      string      `json:"caption"`
	MediaGroupID string      `json:"media_group_id"`
	Photo        []PhotoSize `json:"photo"`
	Document     *Document   `json:"document"`
	Video        *Document   `json:"video"`
	Voice        *Document   `json:"voice"`
	Audio        *Document   `json:"audio"`
	Sticker      *struct{}   `json:"sticker"`
	Origin       *Origin     `json:"forward_origin"`
	// Before Bot API 7.0 (still sent by some servers).
	ForwardFrom       *User  `json:"forward_from"`
	ForwardSenderName string `json:"forward_sender_name"`
	ForwardFromChat   *Chat  `json:"forward_from_chat"`
	ForwardDate       int64  `json:"forward_date"`
}

// User is a Telegram account.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// Name is how the account shows: its first and last name, else @username.
func (u *User) Name() string {
	if u == nil {
		return ""
	}
	if n := strings.TrimSpace(u.FirstName + " " + u.LastName); n != "" {
		return n
	}
	if u.Username != "" {
		return "@" + u.Username
	}
	return "someone"
}

// Chat is where a message was sent.
type Chat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"` // private, group, supergroup, channel
	Title string `json:"title"`
}

// PhotoSize is one size of a photo; Telegram sends several, largest last.
type PhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int64  `json:"file_size"`
}

// Document is a file sent as a file (also used for video, voice and audio).
type Document struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

// Origin says who wrote a forwarded message, and when.
type Origin struct {
	Type            string `json:"type"` // user, hidden_user, chat, channel
	Date            int64  `json:"date"`
	SenderUser      *User  `json:"sender_user"`
	SenderUserName  string `json:"sender_user_name"`
	SenderChat      *Chat  `json:"sender_chat"`
	Chat            *Chat  `json:"chat"`
	AuthorSignature string `json:"author_signature"`
}
