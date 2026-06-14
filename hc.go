package hc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type transport struct {
	Config
}

func (t *transport) Log(s ...any) {
	if t.LogEnabled {
		out := s
		if t.LogPrefix != "" {
			out = append([]any{t.LogPrefix}, out...)
		}

		logger := log.Default()
		if t.Logger != nil {
			logger = t.Logger
		}

		logger.Println(out...)
	}
}

func (t *transport) RoundTrip(req *http.Request) (res *http.Response, err error) {
	if req.URL.Hostname() == "" {
		parsed, err := url.Parse(t.Config.BaseURL)
		if err == nil {
			req.URL.Host = parsed.Host
			req.Host = parsed.Host

			req.URL.Scheme = "https"
			if parsed.Scheme != "" {
				req.URL.Scheme = parsed.Scheme
			}

			if parsed.Port() != "" {
				req.URL.Host = fmt.Sprintf("%s:%s", parsed.Hostname(), parsed.Port())
				req.Host = fmt.Sprintf("%s:%s", parsed.Hostname(), parsed.Port())
			}

			req.URL.Path = strings.TrimRight(parsed.Path, "/") + req.URL.Path
		}
	}

	if t.Interceptor != nil {
		if err = t.Interceptor(req); err != nil {
			if intercept, ok := err.(*Interceptor); ok && intercept.Error() == "" {
				return intercept.TakeOver(req)
			}

			return nil, err
		}
	}

	uri := req.URL.Path
	if req.URL.RawQuery != "" {
		uri = fmt.Sprintf("%s?%s", req.URL.Path, req.URL.RawQuery)
	}

	if t.Config.Timeout <= 0 {
		t.Config.Timeout = 30
	}

	var bodyBytes []byte
	if req.Body != nil && t.Config.MaxRetries > 0 {
		bodyBytes, err = io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}
	}

	for attempt := 0; attempt <= t.Config.MaxRetries; attempt++ {
		if attempt > 0 {
			if bodyBytes != nil {
				req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(t.Config.RetryDelay):
			}
		}

		transp := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: t.Config.InsecureSkipVerify,
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DialContext: func() func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialer := &net.Dialer{
					Timeout:   time.Duration(t.Config.Timeout) * time.Second,
					KeepAlive: time.Duration(t.Config.Timeout) * time.Second,
				}

				return dialer.DialContext
			}(),
		}

		if t.Config.LogEnabled {
			t.Log("[HTTP] >>", req.Method, uri)

			if t.Config.LogHeaderEnabled {
				for key, values := range req.Header {
					for _, val := range values {
						t.Log("[HTTP] >> ", key+":", val)
					}
				}
			}

			if t.Config.LogResponseBodyEnabled && req.Body != nil {
				bBody, err := io.ReadAll(req.Body)
				if err == nil {
					if !strings.Contains(req.Header.Get("content-type"), "multipart") {
						t.Log("[HTTP] >>", string(bBody))
					}
					req.Body = io.NopCloser(bytes.NewBuffer(bBody))
				}
			}
		}

		res, err = transp.RoundTrip(req)

		if t.Config.LogEnabled {
			messages := []any{"[HTTP] <<", req.Method, uri, err}

			if err == nil {
				messages[3] = res.StatusCode
				t.Log(messages...)
				if t.LogResponseBodyEnabled {
					if res.StatusCode >= 200 {
						bBody, err := io.ReadAll(res.Body)
						if err == nil {
							defer res.Body.Close()
							t.Log("[HTTP] <<", string(bBody))
							res.Body = io.NopCloser(bytes.NewBuffer(bBody))
						}
					}
				}
			}
		}

		if err == nil && res.StatusCode < 500 {
			return res, nil
		}

		if attempt < t.Config.MaxRetries {
			if t.Config.RetryCondition != nil && !t.Config.RetryCondition(res, err) {
				return res, err
			}
		}
	}

	return res, err
}

// Interceptor is an error that can be used to intercept a request
type Interceptor struct {
	ErrorMessage string
	TakeOver     func(req *http.Request) (res *http.Response, err error)
}

// Error returns the error message
func (h *Interceptor) Error() string {
	return h.ErrorMessage
}

// Config http client config
type Config struct {
	LogEnabled             bool                                // Enable log
	LogResponseBodyEnabled bool                                // Enable log for response body
	LogHeaderEnabled       bool                                // Enable header logging
	LogPrefix              string                              // Log Prefix
	Logger                 *log.Logger                         // Logger instance
	Interceptor            func(req *http.Request) error       // Intercept request
	Timeout                int                                 // Timeout seconds
	BaseURL                string                              // Base URL
	InsecureSkipVerify     bool                                // Skip TLS certificate verification (not recommended for production)
	MaxRetries             int                                 // Maximum number of retry attempts (default: 0 = no retry)
	RetryDelay             time.Duration                       // Delay between retries
	RetryCondition         func(res *http.Response, err error) bool // Custom retry condition (default: retry on error or status >= 500)
}

// New create new http client
func New(configs ...Config) *http.Client {
	config := Config{}
	if len(configs) > 0 {
		config = configs[0]
	}

	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}

	timeout := config.Timeout
	if config.Timeout < 1 {
		timeout = 30
	}

	return &http.Client{
		Transport: &transport{
			Config: config,
		},
		Timeout: time.Second * time.Duration(timeout),
	}
}

// JSONResponse unmarshal response body to json
func JSONResponse(res *http.Response, obj any) error {
	var err error
	var body []byte
	body, err = io.ReadAll(res.Body)
	if err == nil {
		defer res.Body.Close()
		err = json.Unmarshal(body, obj)
	}

	return err
}
