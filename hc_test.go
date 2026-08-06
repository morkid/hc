package hc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNew(t *testing.T) {
	client := New(Config{
		LogEnabled:             true,
		LogResponseBodyEnabled: true,
		LogHeaderEnabled:       true,
		LogPrefix:              "[TEST]",
		Logger:                 log.New(io.Discard, "", log.LstdFlags),
		Interceptor: func(req *http.Request) error {
			req.Header.Add("Accept", "application/json")
			if req.URL.Path == "/example" {
				return errors.New("invalid request")
			}

			if req.URL.Path == "/hello" {
				return &Interceptor{
					TakeOver: func(req *http.Request) (res *http.Response, err error) {
						return &http.Response{
							Body:       io.NopCloser(strings.NewReader(`{"message":"hello world"}`)),
							Status:     "201 Created",
							StatusCode: 201,
							Proto:      "HTTP/1.1",
							Header:     http.Header{"content-type": []string{"application/json"}},
						}, nil
					},
				}
			}

			if req.URL.Path == "/error" {
				return errors.New("dummy error")
			}

			return nil
		},
	})

	req, err := http.NewRequest("GET", "https://dummyjson.com/products?limit=1", nil)
	assert.Equal(t, nil, err)

	res, err := client.Do(req)
	assert.Equal(t, nil, err)
	assert.Equal(t, 200, res.StatusCode)

	result := map[string]any{}
	err = JSONResponse(res, &result)
	assert.Equal(t, nil, err)
	assert.Equal(t, true, len(result["products"].([]any)) > 0)

	req, err = http.NewRequest("GET", "https://dummyjson.com/hello", nil)
	assert.Equal(t, nil, err)

	res, err = client.Do(req)
	assert.Equal(t, nil, err)
	assert.Equal(t, 201, res.StatusCode)

	req, err = http.NewRequest("GET", "https://dummyjson.com/example", nil)
	assert.Equal(t, nil, err)
	res, err = client.Do(req)
	assert.Equal(t, false, err == nil)

	req, err = http.NewRequest("GET", "https://dummyjson.com/error", nil)
	assert.Equal(t, true, err == nil)
	res, err = client.Do(req)
	assert.Equal(t, true, res == nil)
	assert.Equal(t, false, err == nil)

	client = New(Config{
		LogEnabled:             false,
		LogResponseBodyEnabled: false,
		Timeout:                1,
	})

	req, err = http.NewRequest("GET", "https://xdummyjson.com/products", nil)
	assert.Equal(t, nil, err)

	_, err = client.Do(req)
	assert.Equal(t, false, err == nil)
}

type httpTestWriter struct {
	write func(input []byte)
}

func (h *httpTestWriter) Write(b []byte) (int, error) {
	h.write(b)
	return len(b), nil
}

var _ io.Writer = &httpTestWriter{}

func TestNewHttpClientReal(t *testing.T) {

	client := New(Config{
		LogEnabled:             true,
		LogResponseBodyEnabled: true,
		LogPrefix:              "[TEST]",
		Logger: log.New(&httpTestWriter{
			write: func(input []byte) {
				t.Log(string(input))
			},
		}, "", log.LstdFlags),
	})

	payload := `{
		"title": "Example 1235432"
	}`

	req, _ := http.NewRequest("POST", "https://dummyjson.com/products/add", strings.NewReader(payload))
	req.Header.Add("content-type", "application/json")

	res, err := client.Do(req)
	assert.Equal(t, nil, err)
	assert.Equal(t, 201, res.StatusCode)
}

func TestRetryOnServerError(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{
		MaxRetries: 2,
		RetryDelay: 5 * time.Millisecond,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 503, res.StatusCode)
	assert.Equal(t, 3, attempts)
}

func TestRetryOnConnectionError(t *testing.T) {
	client := New(Config{
		MaxRetries: 1,
		RetryDelay: 5 * time.Millisecond,
		Timeout:    1,
	})

	req, _ := http.NewRequest("GET", "http://127.0.0.1:1", nil)
	_, err := client.Do(req)
	assert.Error(t, err)
}

func TestRetryCustomConditionNoRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{
		MaxRetries:     3,
		RetryDelay:     5 * time.Millisecond,
		RetryCondition: func(res *http.Response, err error) bool { return false },
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 503, res.StatusCode)
}

func TestRetryWithBody(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "trigger") && attempts <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		MaxRetries: 2,
		RetryDelay: 5 * time.Millisecond,
	})

	req, _ := http.NewRequest("POST", server.URL, strings.NewReader(`{"key":"trigger"}`))
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)
	assert.Equal(t, 3, attempts)
}

func TestRetryContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	client := New(Config{
		MaxRetries: 3,
		RetryDelay: 100 * time.Millisecond,
	})

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	_, err := client.Do(req)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestRetryCustomConditionRetry(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{
		MaxRetries: 1,
		RetryDelay: 5 * time.Millisecond,
		RetryCondition: func(res *http.Response, err error) bool {
			return true
		},
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 503, res.StatusCode)
	assert.Equal(t, 2, attempts)
}

func TestNegativeMaxRetries(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{
		MaxRetries: -1,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 503, res.StatusCode)
	assert.Equal(t, 1, attempts)
}

func TestNoRetryByDefault(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{})
	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 503, res.StatusCode)
	assert.Equal(t, 1, attempts)
}

func TestInsecureSkipVerify(t *testing.T) {
	client := New(Config{
		InsecureSkipVerify: true,
		Timeout:            1,
	})

	req, _ := http.NewRequest("GET", "https://localhost:9999", nil)
	_, err := client.Do(req)
	// must fail (connection refused), not TLS error
	assert.Error(t, err)
}

func TestBaseURL(t *testing.T) {
	client := New(Config{
		BaseURL: "https://dummyjson.com:443/",
		Interceptor: func(req *http.Request) error {
			t.Log("Interceptor called", req.URL.String())
			return nil
		},
	})

	req, _ := http.NewRequest("GET", "/products", nil)
	res, err := client.Do(req)
	assert.Equal(t, nil, err)
	assert.Equal(t, 200, res.StatusCode)
}

func TestJSONLogError(t *testing.T) {
	j := &JSONLog{ErrorMessage: "test error"}
	assert.Equal(t, "test error", j.Error())

	j2 := &JSONLog{}
	assert.Equal(t, "", j2.Error())
}

func TestLogSingleJSONEnabled(t *testing.T) {
	var logOutput bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(Config{
		LogEnabled:             true,
		LogSingleJSONEnabled:   true,
		LogResponseBodyEnabled: true,
		LogHeaderEnabled:       true,
		Logger:                 log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("POST", server.URL, strings.NewReader(`{"key":"val"}`))
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)

	output := logOutput.String()
	assert.Contains(t, output, `"method":"POST"`)
	assert.Contains(t, output, `"status_code":200`)
	assert.Contains(t, output, `"request_body":"{\"key\":\"val\"}"`)
	assert.Contains(t, output, `"response_body":"{\"ok\":true}"`)
	assert.Contains(t, output, `"duration_ms"`)
	assert.Contains(t, output, `"attempts":1`)
	assert.Contains(t, output, `"request_headers"`)
	assert.Contains(t, output, `"response_headers"`)
	assert.Contains(t, output, `"error_message":""`)
}

func TestLogSingleJSONEnabledInterceptorError(t *testing.T) {
	var logOutput bytes.Buffer

	client := New(Config{
		LogEnabled:           true,
		LogSingleJSONEnabled: true,
		Logger:               log.New(&logOutput, "", 0),
		Interceptor: func(req *http.Request) error {
			return errors.New("interceptor rejected")
		},
	})

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	res, err := client.Do(req)
	assert.Error(t, err)
	assert.Nil(t, res)

	output := logOutput.String()
	assert.Contains(t, output, `"url":"https://example.com"`)
	assert.Contains(t, output, `"error_message":"interceptor rejected"`)
	assert.Contains(t, output, `"raw_error"`)
}

func TestLogSingleJSONEnabledTakeOver(t *testing.T) {
	var logOutput bytes.Buffer

	client := New(Config{
		LogEnabled:           true,
		LogSingleJSONEnabled: true,
		LogHeaderEnabled:     true,
		Logger:               log.New(&logOutput, "", 0),
		Interceptor: func(req *http.Request) error {
			return &Interceptor{
				TakeOver: func(req *http.Request) (res *http.Response, err error) {
					return &http.Response{
						Body:       io.NopCloser(strings.NewReader(`{"mock":true}`)),
						StatusCode: 201,
						Status:     "201 Created",
						Proto:      "HTTP/1.1",
					}, nil
				},
			}
		},
	})

	req, _ := http.NewRequest("GET", "https://example.com/hello", nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 201, res.StatusCode)

	output := logOutput.String()
	assert.Contains(t, output, `"url":"https://example.com/hello"`)
	assert.Contains(t, output, `"status_code":201`)
	assert.Contains(t, output, `"error_message":""`)
}

func TestLogSingleJSONEnabledWithRetry(t *testing.T) {
	var logOutput bytes.Buffer
	var attempts int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(Config{
		LogEnabled:             true,
		LogSingleJSONEnabled:   true,
		LogResponseBodyEnabled: true,
		MaxRetries:             3,
		RetryDelay:             5 * time.Millisecond,
		Logger:                 log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)

	output := logOutput.String()
	assert.Contains(t, output, `"attempts":3`)
	assert.Contains(t, output, `"status_code":200`)
}

func TestLogSingleJSONEnabledContextCancel(t *testing.T) {
	var logOutput bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())

	client := New(Config{
		LogEnabled:           true,
		LogSingleJSONEnabled: true,
		MaxRetries:           3,
		RetryDelay:           100 * time.Millisecond,
		Logger:               log.New(&logOutput, "", 0),
	})

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	_, err := client.Do(req)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	output := logOutput.String()
	assert.Contains(t, output, `"error_message":"context canceled"`)
}

func TestLogRequestBodyDisabled(t *testing.T) {
	var logOutput bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(Config{
		LogEnabled:             true,
		LogResponseBodyEnabled: true,
		LogRequestBodyDisabled: true,
		Logger:                 log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("POST", server.URL, strings.NewReader(`{"key":"val"}`))
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)

	output := logOutput.String()
	assert.NotContains(t, output, `{"key":"val"}`)
}

func TestLogRequestBodyNotDisabled(t *testing.T) {
	var logOutput bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		LogEnabled:             true,
		LogResponseBodyEnabled: true,
		Logger:                 log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("POST", server.URL, strings.NewReader(`visible-body`))

	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)

	output := logOutput.String()
	assert.Contains(t, output, "visible-body")
}

func TestJSONLogRawErrorNil(t *testing.T) {
	j := &JSONLog{}
	data, err := json.Marshal(j)
	assert.Nil(t, err)
	assert.Contains(t, string(data), `"raw_error":null`)
}

func TestForceAttemptHTTP2Disabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		ForceAttemptHTTP2Disabled: true,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)
}

func TestMaxIdleConnsConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		MaxIdleConns: 50,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)
}

func TestIdleConnTimeoutSecondConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		IdleConnTimeoutSecond: 60,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)
}

func TestTLSHandshakeTimeoutSecondConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		TLSHandshakeTimeoutSecond: 15,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)
}

func TestExpectContinueTimeoutSecondConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		ExpectContinueTimeoutSecond: 2,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)
}

func TestTransportConfigDefaults(t *testing.T) {
	// Test that zero values use defaults (request still succeeds)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := New(Config{
		MaxIdleConns:                0,
		IdleConnTimeoutSecond:       0,
		TLSHandshakeTimeoutSecond:   0,
		ExpectContinueTimeoutSecond: 0,
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)
}

func TestLogSingleJSONEnabledTransportError(t *testing.T) {
	var logOutput bytes.Buffer

	client := New(Config{
		LogEnabled:           true,
		LogSingleJSONEnabled: true,
		Timeout:              1,
		Logger:               log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("GET", "http://127.0.0.1:1", nil)
	_, err := client.Do(req)
	assert.Error(t, err)

	output := logOutput.String()
	assert.Contains(t, output, `"error_message"`)
	assert.Contains(t, output, `"raw_error"`)
}

func TestLogSingleJSONEnabledRetryConditionNoRetry(t *testing.T) {
	var logOutput bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{
		LogEnabled:           true,
		LogSingleJSONEnabled: true,
		MaxRetries:           3,
		RetryDelay:           5 * time.Millisecond,
		RetryCondition:       func(res *http.Response, err error) bool { return false },
		Logger:               log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 503, res.StatusCode)

	output := logOutput.String()
	assert.Contains(t, output, `"status_code":503`)
	assert.Contains(t, output, `"attempts":1`)
}

func TestLogSingleJSONEnabledRequestBodyDisabled(t *testing.T) {
	var logOutput bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(Config{
		LogEnabled:             true,
		LogSingleJSONEnabled:   true,
		LogResponseBodyEnabled: true,
		LogRequestBodyDisabled: true,
		Logger:                 log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("POST", server.URL, strings.NewReader(`hidden-body`))

	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 200, res.StatusCode)

	output := logOutput.String()
	assert.NotContains(t, output, "hidden-body")
	assert.Contains(t, output, `"response_body":"{\"ok\":true}"`)
}

func TestLogSingleJSONEnabledRetryExhausted(t *testing.T) {
	var logOutput bytes.Buffer

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := New(Config{
		LogEnabled:           true,
		LogSingleJSONEnabled: true,
		MaxRetries:           2,
		RetryDelay:           5 * time.Millisecond,
		Logger:               log.New(&logOutput, "", 0),
	})

	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := client.Do(req)
	assert.Nil(t, err)
	assert.Equal(t, 503, res.StatusCode)

	output := logOutput.String()
	assert.Contains(t, output, `"status_code":503`)
	assert.Contains(t, output, `"attempts":3`)
}
