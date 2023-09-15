package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/pkg/errors"

	"github.com/CoreumFoundation/coreum-tools/pkg/retry"
)

// RetryableClientConfig is the config for the RetryableClient.
type RetryableClientConfig struct {
	RequestTimeout time.Duration
	DoTimeout      time.Duration
	RetryDelay     time.Duration
}

// DefaultClientConfig returns default RetryableClientConfig.
func DefaultClientConfig() RetryableClientConfig {
	return RetryableClientConfig{
		RequestTimeout: 5 * time.Second,
		DoTimeout:      30 * time.Second,
		RetryDelay:     300 * time.Millisecond,
	}
}

// RetryableClient is HTTP RetryableClient.
type RetryableClient struct {
	cfg RetryableClientConfig
}

// NewRetryableClient returns new instance RetryableClient.
func NewRetryableClient(cfg RetryableClientConfig) RetryableClient {
	return RetryableClient{
		cfg: cfg,
	}
}

// DoJSON executes the HTTP application/json request with retires based on the client configuration.
func (c RetryableClient) DoJSON(ctx context.Context, method, url string, reqBody any, resDecoder func([]byte) error) error {
	doCtx, doCtxCancel := context.WithTimeout(ctx, c.cfg.DoTimeout)
	defer doCtxCancel()
	return retry.Do(doCtx, c.cfg.RetryDelay, func() error {
		reqCtx, reqCtxCancel := context.WithTimeout(ctx, c.cfg.RequestTimeout)
		defer reqCtxCancel()

		return doJSON(reqCtx, method, url, reqBody, resDecoder)
	})
}

func doJSON(ctx context.Context, method, url string, reqBody any, resDecoder func([]byte) error) error {
	reqBodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return errors.Errorf("failed to marshal request body, err: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(reqBodyBytes))
	if err != nil {
		return errors.Errorf("failed to build the request, err: %v", err)
	}

	// fix for the EOF error
	req.Close = true
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return retry.Retryable(errors.Errorf("failed to perform the request, err: %v", err))
	}

	defer resp.Body.Close()
	bodyData, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.Errorf("failed to read the response body, err: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return retry.Retryable(errors.Errorf("failed to perform request, code: %d, body: %s", resp.StatusCode, string(bodyData)))
	}

	return resDecoder(bodyData)
}
