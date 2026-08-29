// Package healthcheck implements the self-contained container readiness probe.
package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAddress = ":8080"
	probeTimeout   = 2 * time.Second
)

// Check requests the local readiness endpoint derived from the listen address.
func Check(ctx context.Context, address string) error {
	target, err := targetURL(address)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("create readiness request: %w", err)
	}
	client := &http.Client{
		Timeout: probeTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request readiness endpoint: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4*1024))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness endpoint returned %s", response.Status)
	}
	return nil
}

func targetURL(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		address = defaultAddress
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("parse listen address %q: %w", address, err)
	}
	if strings.TrimSpace(port) == "" {
		return "", errors.New("listen address port is empty")
	}
	portNumber, err := net.LookupPort("tcp", port)
	if err != nil {
		return "", fmt.Errorf("resolve listen port %q: %w", port, err)
	}
	port = strconv.Itoa(portNumber)
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, port),
		Path:   "/readyz",
	}).String(), nil
}
