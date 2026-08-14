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

func (t *transport) logJSON(j *JSONLog) (err error) {
	if j != nil {
		j.finishedAt = time.Now()
		j.DurationMS = j.finishedAt.Sub(j.startedAt).Milliseconds()
		if err = j.RawError; err != nil {
			j.ErrorMessage = err.Error()
		}

		if t.Config.LogSingleJSONEnabled {
			if b, e := json.Marshal(j); e == nil {
				t.Log(string(b))
			}
		}

	}

	return err
}

func (t *transport) logType(logType string, s ...any) {

	switch logType {
	case "in":
		s = append([]any{"<<"}, s...)
	case "out":
		s = append([]any{">>"}, s...)
	}

	if !t.LogHTTPPrefixDisabled {
		s = append([]any{"[HTTP]"}, s...)
	}

	if !t.Config.LogSingleJSONEnabled {
		t.Log(s...)
	}
}

func (t *transport) RoundTrip(req *http.Request) (res *http.Response, err error) {
	start := time.Now()

	jsonLog := new(JSONLog)
	jsonLog.Method = req.Method
	jsonLog.startedAt = start

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

	uri := req.URL.String()
	jsonLog.URL = uri

	if t.Interceptor != nil {
		if err = t.Interceptor(req); err != nil {
			if intercept, ok := err.(*Interceptor); ok && intercept.Error() == "" {
				res, err := intercept.TakeOver(req)
				t.logType("out", req.Method, uri)

				if t.Config.LogHeaderEnabled {
					jsonLog.RequestHeaders = map[string]string{}
					for key, values := range req.Header {
						t.logType("out", " "+key+":", strings.Join(values, "; "))
						jsonLog.RequestHeaders[key] = strings.Join(values, "; ")
					}
				}

				messages := []any{req.Method, uri, err}
				jsonLog.RawError = err

				if err == nil {
					messages[2] = res.StatusCode
					t.logType("in", messages...)
					jsonLog.StatusCode = res.StatusCode
					if t.Config.LogHeaderEnabled {
						jsonLog.ResponseHeaders = map[string]string{}
						for key, values := range res.Header {
							t.logType("in", " "+key+":", strings.Join(values, "; "))
							jsonLog.ResponseHeaders[key] = strings.Join(values, "; ")
						}
					}
				}

				return res, t.logJSON(jsonLog)
			}

			jsonLog.RawError = err

			return nil, t.logJSON(jsonLog)
		}
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
		jsonLog.Attempts = attempt + 1

		if attempt > 0 {
			if bodyBytes != nil {
				req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			select {
			case <-req.Context().Done():
				jsonLog.RawError = req.Context().Err()
				t.logJSON(jsonLog)
				return nil, req.Context().Err()
			case <-time.After(t.Config.RetryDelay):
			}
		}

		maxIdleConns := t.Config.MaxIdleConns
		if maxIdleConns <= 0 {
			maxIdleConns = 100
		}
		idleConnTimeout := t.Config.IdleConnTimeoutSecond
		if idleConnTimeout <= 0 {
			idleConnTimeout = 90
		}
		tlsHandshakeTimeout := t.Config.TLSHandshakeTimeoutSecond
		if tlsHandshakeTimeout <= 0 {
			tlsHandshakeTimeout = 10
		}
		expectContinueTimeout := t.Config.ExpectContinueTimeoutSecond
		if expectContinueTimeout <= 0 {
			expectContinueTimeout = 1
		}

		transp := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: t.Config.InsecureSkipVerify,
			},
			ForceAttemptHTTP2:     !t.Config.ForceAttemptHTTP2Disabled,
			MaxIdleConns:          maxIdleConns,
			IdleConnTimeout:       time.Duration(idleConnTimeout) * time.Second,
			TLSHandshakeTimeout:   time.Duration(tlsHandshakeTimeout) * time.Second,
			ExpectContinueTimeout: time.Duration(expectContinueTimeout) * time.Second,
			DialContext: func() func(ctx context.Context, network, addr string) (net.Conn, error) {
				dialer := &net.Dialer{
					Timeout:   time.Duration(t.Config.Timeout) * time.Second,
					KeepAlive: time.Duration(t.Config.Timeout) * time.Second,
				}

				return dialer.DialContext
			}(),
		}

		if t.Config.LogEnabled {
			t.logType("out", req.Method, uri)

			if t.Config.LogHeaderEnabled {
				jsonLog.RequestHeaders = map[string]string{}
				for key, values := range req.Header {
					jsonLog.RequestHeaders[key] = strings.Join(values, "; ")
					t.logType("out", " "+key+":", strings.Join(values, "; "))
				}
			}

			if !t.Config.LogRequestBodyDisabled && req.Body != nil {
				bBody, err := io.ReadAll(req.Body)
				if err == nil {
					if !strings.Contains(req.Header.Get("content-type"), "multipart") {
						t.logType("out", string(bBody))
						jsonLog.RequestBody = string(bBody)
					}
					req.Body = io.NopCloser(bytes.NewBuffer(bBody))
				}
			}
		}

		res, err = transp.RoundTrip(req)

		if t.Config.LogEnabled {
			messages := []any{req.Method, uri, err}

			if err == nil {
				messages[2] = res.StatusCode
				t.logType("in", messages...)
				if t.Config.LogHeaderEnabled {
					jsonLog.ResponseHeaders = map[string]string{}
					for key, values := range res.Header {
						t.logType("in", " "+key+":", strings.Join(values, "; "))
						jsonLog.ResponseHeaders[key] = strings.Join(values, "; ")
					}
				}

				if t.LogResponseBodyEnabled {
					if res.StatusCode >= 200 {
						bBody, err := io.ReadAll(res.Body)
						if err == nil {
							defer res.Body.Close()
							if strings.Contains(res.Header.Get("content-type"), "application/json") {
								t.logType("in", string(bBody))
								jsonLog.ResponseBody = string(bBody)
							}
							res.Body = io.NopCloser(bytes.NewBuffer(bBody))
						}
					}
				}
			}
		}

		if err == nil {
			jsonLog.StatusCode = res.StatusCode
			if res.StatusCode < 500 {
				return res, t.logJSON(jsonLog)
			}
		}

		if attempt < t.Config.MaxRetries {
			if t.Config.RetryCondition != nil && !t.Config.RetryCondition(res, err) {
				jsonLog.RawError = err
				return res, t.logJSON(jsonLog)
			}
		}
	}

	t.logJSON(jsonLog)

	return res, err
}

// JSONLog represents a single JSON log entry for an HTTP request/response cycle.
type JSONLog struct {
	Method          string            `json:"method"`
	URL             string            `json:"url"`
	StatusCode      int               `json:"status_code"`
	DurationMS      int64             `json:"duration_ms"`
	Attempts        int               `json:"attempts"`
	RequestBody     string            `json:"request_body"`
	ResponseBody    string            `json:"response_body"`
	RequestHeaders  map[string]string `json:"request_headers"`
	ResponseHeaders map[string]string `json:"response_headers"`
	ErrorMessage    string            `json:"error_message"`
	RawError        error             `json:"raw_error"`
	startedAt       time.Time         `json:"-"`
	finishedAt      time.Time         `json:"-"`
}

// Error returns the error message, allowing JSONLog to be used as an error.
func (j *JSONLog) Error() string {
	return j.ErrorMessage
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
	LogEnabled                  bool                                     // Enable log
	LogResponseBodyEnabled      bool                                     // Enable log for response body
	LogHeaderEnabled            bool                                     // Enable header logging
	LogPrefix                   string                                   // Log Prefix
	LogHTTPPrefixDisabled       bool                                     // Disable log http prefix
	LogSingleJSONEnabled        bool                                     // Enable single JSON log line
	LogRequestBodyDisabled      bool                                     // Disable request body from logging
	Logger                      *log.Logger                              // Logger instance
	Interceptor                 func(req *http.Request) error            // Intercept request
	Timeout                     int                                      // Timeout seconds
	BaseURL                     string                                   // Base URL
	InsecureSkipVerify          bool                                     // Skip TLS certificate verification (not recommended for production)
	ForceAttemptHTTP2Disabled   bool                                     // Disable HTTP/2
	MaxIdleConns                int                                      // Max idle connections (default: 100)
	IdleConnTimeoutSecond       int                                      // Idle connection timeout seconds (default: 90)
	TLSHandshakeTimeoutSecond   int                                      // TLS handshake timeout seconds (default: 10)
	ExpectContinueTimeoutSecond int                                      // Expect continue timeout seconds (default: 1)
	MaxRetries                  int                                      // Maximum number of retry attempts (default: 0 = no retry)
	RetryDelay                  time.Duration                            // Delay between retries
	RetryCondition              func(res *http.Response, err error) bool // Custom retry condition (default: retry on error or status >= 500)
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
