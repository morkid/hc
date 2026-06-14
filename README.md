# HC (Http Client)

[![test](https://github.com/morkid/hc/actions/workflows/go.yml/badge.svg?label=test)](https://github.com/morkid/hc/actions/workflows/go.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/morkid/hc.svg)](https://pkg.go.dev/github.com/morkid/hc)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Simple HTTP Client for Go with interceptor support.

## Table of Contents

- [Installation](#installation)
- [Features](#features)
- [Usage](#usage)
  - [Basic with Interceptor](#basic-with-interceptor)
  - [BaseURL](#baseurl)
  - [Mocking Response (TakeOver)](#mocking-response-takeover)
  - [JSON Response Helper](#json-response-helper)
- [Configuration](#configuration)
- [Resty Integration](#resty-integration)

## Installation

```bash
go get github.com/morkid/hc
```

## Features

- Interceptor pattern for request/response customization
- Request mocking via `TakeOver`
- Automatic `BaseURL` resolution (supports relative paths)
- Configurable logging (request, response body, headers)
- Custom logger support
- Configurable timeout
- Configurable TLS certificate verification
- JSON response helper
- [Resty](https://github.com/go-resty/resty) integration

## Usage

### Basic with Interceptor

```go
import "github.com/morkid/hc"

client := hc.New(hc.Config{
    BaseURL: "http://example.com",
    LogEnabled: true,
    Interceptor: func(req *http.Request) error {
        req.Header.Add("Accept", "application/json")

        // forward to the original request
        return nil
    },
})

req, _ := http.NewRequest("GET", "/hello-world.json", nil)
res, err := client.Do(req)
log.Println(err)
log.Println(res.StatusCode)
```

### BaseURL

Set a `BaseURL` and use relative paths in your requests. HC will resolve the full URL automatically.

```go
client := hc.New(hc.Config{
    BaseURL: "https://dummyjson.com:443/",
})

req, _ := http.NewRequest("GET", "/products", nil)
res, _ := client.Do(req)
```

### Mocking Response (TakeOver)

The interceptor has two modes:

1. **Return an error** — the request is rejected with that error.
2. **Return an `*hc.Interceptor` with empty `ErrorMessage` and a `TakeOver` function** — the request is intercepted and the `TakeOver` function provides a mock response.

```go
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
    Interceptor: func(req *http.Request) error {
        if req.URL.Path == "/hello-world.json" {
            return &helloWorld
        }
        return nil
    },
})

req, _ := http.NewRequest("GET", "/hello-world.json", nil)
res, _ := client.Do(req)
```

### JSON Response Helper

Use `hc.JSONResponse` to unmarshal the response body directly into a struct.

```go
var result map[string]any
err := hc.JSONResponse(res, &result)
```

## Configuration

| Field                   | Type                           | Description                              |
| ----------------------- | ------------------------------ | ---------------------------------------- |
| `LogEnabled`            | `bool`                         | Enable request/response logging          |
| `LogResponseBodyEnabled`| `bool`                         | Enable response body logging             |
| `LogHeaderEnabled`      | `bool`                         | Enable request header logging            |
| `LogPrefix`             | `string`                       | Prefix for log messages                  |
| `Logger`                | `*log.Logger`                  | Custom logger instance                   |
| `Interceptor`           | `func(*http.Request) error`    | Intercept and customize requests         |
| `Timeout`               | `int`                          | Timeout in seconds (default: 30)         |
| `BaseURL`               | `string`                       | Base URL for relative path resolution    |
| `InsecureSkipVerify`    | `bool`                         | Skip TLS certificate verification       |

## Resty Integration

HC's transport can be dropped into [Resty](https://github.com/go-resty/resty), so you get HC's interceptor, logging, and base URL features through Resty's fluent API.

```go
client := hc.New()

restyClient := resty.New()
restyClient.SetTransport(client.Transport)

res, err := restyClient.R().
    SetHeader("Accept", "application/json").
    Get("http://example.com/hello-world.json")
```
