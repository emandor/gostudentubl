package httpx

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"

	retry "github.com/hashicorp/go-retryablehttp"
)

func NewHTTP(timeout time.Duration) (*http.Client, error) {
	jar, _ := cookiejar.New(nil)

	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}

	rc := retry.NewClient()
	rc.RetryMax = 3
	rc.RetryWaitMin = 500 * time.Millisecond
	rc.RetryWaitMax = 2 * time.Second
	rc.HTTPClient = &http.Client{Timeout: timeout, Transport: transport, Jar: jar}

	return rc.StandardClient(), nil
}
