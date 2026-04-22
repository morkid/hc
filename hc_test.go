package hc

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

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
	err = JsonResponse(res, &result)
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
