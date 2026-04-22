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

	transp := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
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
			bodyBytes, err := io.ReadAll(req.Body)
			if err == nil {
				if !strings.Contains(req.Header.Get("content-type"), "multipart") {
					t.Log("[HTTP] >>", string(bodyBytes))
				}
				req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
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
					bodyBytes, err := io.ReadAll(res.Body)
					if err == nil {
						defer res.Body.Close()
						t.Log("[HTTP] <<", string(bodyBytes))
						res.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
					}
				}
			}
		}
	}

	return res, err
}

type Interceptor struct {
	ErrorMessage string
	TakeOver     func(req *http.Request) (res *http.Response, err error)
}

func (h *Interceptor) Error() string {
	return h.ErrorMessage
}

type Config struct {
	LogEnabled             bool                          // Enable log
	LogResponseBodyEnabled bool                          // Enable log for response body
	LogHeaderEnabled       bool                          // Enable header logging
	LogPrefix              string                        // Log Prefix
	Logger                 *log.Logger                   // Logger instance
	Interceptor            func(req *http.Request) error // Intercept request
	Timeout                int                           // Timeout seconds
}

// New create new http client
func New(configs ...Config) *http.Client {
	config := Config{}
	if len(configs) > 0 {
		config = configs[0]
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

func JsonResponse(res *http.Response, obj any) error {
	var err error
	var body []byte
	body, err = io.ReadAll(res.Body)
	if err == nil {
		defer res.Body.Close()
		err = json.Unmarshal(body, obj)
	}

	return err
}
