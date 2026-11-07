// Package api is a minimal client for the Namecheap XML API (read-only methods).
package api

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/idarciooliveira/namecheap-cli/internal/ip"
)

const (
	ProductionURL = "https://api.namecheap.com/xml.response"
	SandboxURL    = "https://api.sandbox.namecheap.com/xml.response"
)

type Client struct {
	BaseURL  string
	APIUser  string
	APIKey   string
	Username string
	ClientIP string
	HTTP     *http.Client
	// RateLimit caps calls per minute from this process. Namecheap allows 50/min per key.
	RateLimit int
	// Backoff is the base delay between retries.
	Backoff time.Duration

	mu    sync.Mutex
	calls []time.Time
}

func New(baseURL, apiUser, apiKey, username, clientIP string) *Client {
	return &Client{
		BaseURL:   baseURL,
		APIUser:   apiUser,
		APIKey:    apiKey,
		Username:  username,
		ClientIP:  clientIP,
		HTTP:      &http.Client{Transport: ip.Transport(), Timeout: 60 * time.Second},
		RateLimit: 45,
		Backoff:   2 * time.Second,
	}
}

// Error is an error returned by the Namecheap API itself.
type Error struct {
	Command string
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (code %s)", e.Message, e.Code)
}

// IsAuth reports whether the error is a global auth error: bad API user/key,
// username, or a client IP that is not whitelisted (10xxxxx codes).
func (e *Error) IsAuth() bool {
	return len(e.Code) == 7 && strings.HasPrefix(e.Code, "10")
}

// IsIP reports whether the error is about the client IP / whitelist.
func (e *Error) IsIP() bool {
	switch e.Code {
	case "1011150", "1017150", "1017105", "1010105", "1011105":
		return true
	}
	return false
}

type envelope struct {
	Status string `xml:"Status,attr"`
	Errors []struct {
		Number string `xml:"Number,attr"`
		Text   string `xml:",chardata"`
	} `xml:"Errors>Error"`
	CommandResponse struct {
		Inner []byte `xml:",innerxml"`
	} `xml:"CommandResponse"`
}

// Call runs a command and decodes the children of <CommandResponse> into out.
// out's fields are matched against a synthetic root wrapping those children.
func (c *Client) Call(ctx context.Context, command string, params url.Values, out any) error {
	q := url.Values{}
	for k, v := range params {
		q[k] = v
	}
	q.Set("ApiUser", c.APIUser)
	q.Set("ApiKey", c.APIKey)
	q.Set("UserName", c.Username)
	q.Set("ClientIp", c.ClientIP)
	q.Set("Command", command)

	body, err := c.get(ctx, c.BaseURL+"?"+q.Encode())
	if err != nil {
		return err
	}
	var env envelope
	if err := xml.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("%s: unreadable response from Namecheap: %v", command, err)
	}
	if !strings.EqualFold(env.Status, "OK") {
		e := &Error{Command: command, Code: "unknown", Message: "Namecheap returned an error"}
		if len(env.Errors) > 0 {
			e.Code = strings.TrimSpace(env.Errors[0].Number)
			e.Message = strings.TrimSpace(env.Errors[0].Text)
		}
		return e
	}
	if out == nil {
		return nil
	}
	wrapped := make([]byte, 0, len(env.CommandResponse.Inner)+7)
	wrapped = append(wrapped, "<r>"...)
	wrapped = append(wrapped, env.CommandResponse.Inner...)
	wrapped = append(wrapped, "</r>"...)
	if err := xml.Unmarshal(wrapped, out); err != nil {
		return fmt.Errorf("%s: unexpected response shape: %v", command, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * c.Backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if err := c.wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, errors.New("could not build Namecheap request")
		}
		req.Header.Set("User-Agent", "namecheap-cli")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			// Never surface the URL: it contains the API key.
			lastErr = scrub(err)
			if ctx.Err() != nil {
				return nil, lastErr
			}
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("reading Namecheap response: %v", err)
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("Namecheap returned HTTP %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("Namecheap returned HTTP %d", resp.StatusCode)
		}
		return b, nil
	}
	return nil, lastErr
}

func scrub(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("request to Namecheap failed: %v", ue.Err)
	}
	return errors.New("request to Namecheap failed")
}

// wait blocks until a call fits in the per-minute budget.
func (c *Client) wait(ctx context.Context) error {
	for {
		c.mu.Lock()
		now := time.Now()
		cut := now.Add(-time.Minute)
		i := 0
		for i < len(c.calls) && c.calls[i].Before(cut) {
			i++
		}
		c.calls = c.calls[i:]
		if c.RateLimit <= 0 || len(c.calls) < c.RateLimit {
			c.calls = append(c.calls, now)
			c.mu.Unlock()
			return nil
		}
		d := c.calls[0].Sub(cut)
		c.mu.Unlock()
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Bool decodes Namecheap's loosely formatted booleans ("true", "True", "", ...).
type Bool bool

func (b *Bool) UnmarshalXMLAttr(attr xml.Attr) error {
	switch strings.ToLower(strings.TrimSpace(attr.Value)) {
	case "true", "yes", "1", "enabled":
		*b = true
	default:
		*b = false
	}
	return nil
}

func (b *Bool) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var s string
	if err := d.DecodeElement(&s, &start); err != nil {
		return err
	}
	return b.UnmarshalXMLAttr(xml.Attr{Value: s})
}
