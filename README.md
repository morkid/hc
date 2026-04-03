# HC (Http Client)

[![test](https://github.com/morkid/hc/actions/workflows/go.yml/badge.svg?label=test)](https://github.com/morkid/hc/actions/workflows/go.yml)

Simple HTTP Client for go with interceptor support.

```go
import "github.com/morkid/hc"

helloWorld := hc.Interceptor{
    TakeOver: func(req *http.Request) (res *http.Response, err error) {
        return &http.Response{
            Body:       io.NopCloser(strings.NewReader(`{"message":"hello world"}`)),
            Status:     "200 OK",
            StatusCode: 200,
            Proto:      "HTTP/1.1",
        }, nil
    },
}

client := hc.New(hc.Config{
    LogEnabled: true,
    Interceptor: func (req *http.Request) error {
        // customize http header
        req.Header.Add("Accept", "application/json")

        if req.URL.Path == "/hello-world.json" {
            // mock response
            return &helloWorld
        }

        // forward to the original request
        return nil
    }
})

req, _ := http.NewRequest("GET", "http://example.com/hello-world.json", nil)
res, err := client.Do(req)
log.Println(err)
log.Println(res.StatusCode)

```

## [Resty](https://github.com/go-resty/resty) integration:

```go

client := hc.New()

restyClient := resty.New()

restyClient.SetTransport(client.Transport)

res, err := restyClient.R().Get("http://example.com/hello-world.json")

```