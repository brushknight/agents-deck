package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Service is OpenCode's local HTTP API, from its service file.
type Service struct {
	URL      string
	Password string
	client   *http.Client
}

// APIError is a non-2xx answer from OpenCode's service.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return "opencode api: " + e.Body }

// LoadService reads the running service's URL and password.
func LoadService() (*Service, error) {
	dir := StateHome()
	b, err := os.ReadFile(filepath.Join(dir, "service.json"))
	if err != nil {
		return nil, err
	}
	var s struct {
		URL      string `json:"url"`
		Password string `json:"password"`
	}
	if json.Unmarshal(b, &s) != nil || s.URL == "" || s.Password == "" {
		return nil, errors.New("opencode service file is not usable")
	}
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, errors.New("opencode service file has a bad URL")
	}
	if u.Port() == "" {
		return nil, errors.New("opencode service file has a bad URL")
	}
	if u.Hostname() == "0.0.0.0" || u.Hostname() == "::" || u.Hostname() == "[::]" {
		u.Host = net.JoinHostPort("127.0.0.1", u.Port())
	}
	if !loopbackHost(u.Hostname()) {
		return nil, errors.New("opencode service URL is not loopback")
	}
	if u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("opencode service file has a bad URL")
	}
	return &Service{URL: strings.TrimRight(u.String(), "/"), Password: s.Password, client: serviceClient()}, nil
}

// StateHome is OpenCode's state folder (its service file lives there).
func StateHome() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "opencode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "opencode")
}

func serviceClient() *http.Client {
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Service) call(method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.URL+path, reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", s.Password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := s.client
	if client == nil {
		client = serviceClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Body: resp.Status + " " + strings.TrimSpace(string(out))}
	}
	return out, nil
}

// Active is the sessions the service is running now.
func (s *Service) Active() (map[string]bool, error) {
	out, err := s.call("GET", "/api/session/active", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data map[string]struct {
			Type string `json:"type"`
		} `json:"data"`
	}
	if json.Unmarshal(out, &r) != nil {
		return nil, errors.New("opencode api: bad active answer")
	}
	m := map[string]bool{}
	for id, v := range r.Data {
		if v.Type == "running" {
			m[id] = true
		}
	}
	return m, nil
}

// Permission is a pending request from OpenCode.
type Permission struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Action    string   `json:"action"`
	Resources []string `json:"resources"`
	Save      []string `json:"save"`
	Message   string   `json:"message"`
}

// PermissionsFor lists the requests waiting for you in one session. The
// global permission route is location-scoped, so it can miss other projects.
func (s *Service) PermissionsFor(sessionID string) ([]Permission, error) {
	out, err := s.call("GET", "/api/session/"+sessionID+"/permission", nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Data []Permission `json:"data"`
	}
	if json.Unmarshal(out, &r) != nil {
		return nil, errors.New("opencode api: bad permission answer")
	}
	return r.Data, nil
}

// ReplyPermission answers a pending permission: once, always or reject.
func (s *Service) ReplyPermission(sessionID, requestID, decision string) error {
	_, err := s.call("POST", "/api/session/"+sessionID+"/permission/"+requestID+"/reply",
		map[string]string{"decision": decision})
	return err
}

// Interrupt stops the session's current turn.
func (s *Service) Interrupt(sessionID string) error {
	_, err := s.call("POST", "/api/session/"+sessionID+"/interrupt", nil)
	return err
}

// Prompt sends text to the session.
func (s *Service) Prompt(sessionID, text string) error {
	_, err := s.call("POST", "/api/session/"+sessionID+"/prompt", map[string]any{"text": text})
	return err
}
