# HC (Http Client)

[![test](https://github.com/morkid/hc/actions/workflows/go.yml/badge.svg?label=test)](https://github.com/morkid/hc/actions/workflows/go.yml)

Simple HTTP Client for go with interceptor support.

```go
import "github.com/morkid/hc"

interceptor := hc.Interceptor{
    TakeOver: func(req *http.Request) (res *http.Response, err error) {
        if req.URL.Path == "/hello-world.json" {
            return &http.Response{
                Body:       io.NopCloser(strings.NewReader(`{"message":"hello world"}`)),
                Status:     "200 OK",
                StatusCode: 200,
                Proto:      "HTTP/1.1",
            }, nil
        }

        return &http.Response{
            Body:       io.NopCloser(strings.NewReader(`{"message":"not found"}`)),
            Status:     "404 Not Found",
            StatusCode: 404,
            Proto:      "HTTP/1.1",
        }, nil
    },
}

client := hc.New(hc.Config{
    LogEnabled: true,
    Interceptor: func (req *http.Request) error {
        return &interceptor
    }
})

req, _ := http.NewRequest("GET", "http://example.com/hello-world.json", nil)
res, err := client.Do(req)
log.Println(err)
log.Println(res.StatusCode)

```