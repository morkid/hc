package hc

import (
	"context"
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
		MaxRetries:             3,
		RetryDelay:             5 * time.Millisecond,
		RetryCondition:         func(res *http.Response, err error) bool { return false },
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
