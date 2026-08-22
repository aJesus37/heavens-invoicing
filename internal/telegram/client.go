package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const defaultBaseURL = "https://api.telegram.org"

type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

func NewClient(httpClient *http.Client, baseURL, token string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{httpClient: httpClient, baseURL: baseURL, token: token}
}

func (c *Client) endpoint(method string) string {
	return c.baseURL + "/bot" + c.token + "/" + method
}

func (c *Client) SendMessage(ctx context.Context, chatID, text string) error {
	return c.do(ctx, "sendMessage", func() (*http.Request, error) {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		if err := w.WriteField("chat_id", chatID); err != nil {
			return nil, err
		}
		if err := w.WriteField("text", text); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("sendMessage"), &body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		return req, nil
	}, nil)
}

func (c *Client) SendDocument(ctx context.Context, chatID, filename string, content []byte, caption string) error {
	return c.do(ctx, "sendDocument", func() (*http.Request, error) {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		if err := w.WriteField("chat_id", chatID); err != nil {
			return nil, err
		}
		fw, err := w.CreateFormFile("document", filename)
		if err != nil {
			return nil, err
		}
		if _, err := fw.Write(content); err != nil {
			return nil, err
		}
		if err := w.WriteField("caption", caption); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("sendDocument"), &body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		return req, nil
	}, nil)
}

// Chat identifies the conversation an update came from.
type Chat struct {
	ID int64 `json:"id"`
}

// Message is the subset of a Telegram message relevant to the admin bot.
type Message struct {
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}

// Update is a single Telegram long-polling update.
type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message,omitempty"`
}

// GetUpdates polls pending updates starting at offset (inclusive). timeout=0:
// each call returns immediately, no long-polling hold.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	q := url.Values{}
	q.Set("offset", strconv.FormatInt(offset, 10))
	q.Set("timeout", "0")

	var updates []Update
	err := c.do(ctx, "getUpdates", func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("getUpdates")+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		return req, nil
	}, &updates)
	if err != nil {
		return nil, err
	}
	return updates, nil
}

// do sends the request built by build, applies the shared Telegram API
// response checks (HTTP status, ok flag, description) with token redaction on
// transport errors, and when out is non-nil decodes the envelope's result into it.
func (c *Client) do(ctx context.Context, method string, build func() (*http.Request, error), out any) error {
	req, err := build()
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, redactToken(err, c.token))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, redactToken(err, c.token))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("telegram %s: reading response: %w", method, err)
	}

	var parsed struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	parseErr := json.Unmarshal(raw, &parsed)
	description := ""
	if parseErr == nil {
		description = parsed.Description
	}

	status := resp.StatusCode
	if status < 200 || status > 299 {
		detail := rawSnippet(raw)
		if description != "" {
			detail = description
		}
		return fmt.Errorf("telegram %s failed (HTTP %d): %s", method, status, detail)
	}
	if parseErr != nil {
		return fmt.Errorf("telegram %s failed (HTTP %d): unexpected response: %s", method, status, rawSnippet(raw))
	}
	if !parsed.OK {
		detail := description
		if detail == "" {
			detail = rawSnippet(raw)
		}
		return fmt.Errorf("telegram %s failed: %s", method, detail)
	}
	if out != nil && len(parsed.Result) > 0 {
		if err := json.Unmarshal(parsed.Result, out); err != nil {
			return fmt.Errorf("telegram %s: decoding result: %w", method, err)
		}
	}
	return nil
}

func (c *Client) post(ctx context.Context, method string, fill func(*multipart.Writer) error) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := fill(w); err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(method), &body)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, redactToken(err, c.token))
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, redactToken(err, c.token))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("telegram %s: reading response: %w", method, err)
	}

	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
	}
	parseErr := json.Unmarshal(raw, &parsed)
	description := ""
	if parseErr == nil {
		description = parsed.Description
	}

	status := resp.StatusCode
	if status < 200 || status > 299 {
		detail := rawSnippet(raw)
		if description != "" {
			detail = description
		}
		return fmt.Errorf("telegram %s failed (HTTP %d): %s", method, status, detail)
	}
	if parseErr != nil {
		return fmt.Errorf("telegram %s failed (HTTP %d): unexpected response: %s", method, status, rawSnippet(raw))
	}
	if !parsed.OK {
		detail := description
		if detail == "" {
			detail = rawSnippet(raw)
		}
		return fmt.Errorf("telegram %s failed: %s", method, detail)
	}
	return nil
}

const maxResponseBytes = 2048

// tokenRedacted hides the bot token embedded in transport error messages
// (*url.Error quotes the full request URL, which contains /bot<token>/).
type tokenRedacted struct {
	err   error
	token string
}

func (e *tokenRedacted) Error() string {
	if e.token == "" {
		return e.err.Error()
	}
	return strings.ReplaceAll(e.err.Error(), e.token, "***")
}

func (e *tokenRedacted) Unwrap() error { return e.err }

func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	return &tokenRedacted{err: err, token: token}
}

func rawSnippet(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) > max {
		s = s[:max] + "...(truncated)"
	}
	return s
}
