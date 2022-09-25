package sqsd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Invoker invokes worker process by any way.
type Invoker interface {
	Invoke(context.Context, Message) error
}

// HTTPInvoker invokes worker process by HTTP POST request.
type HTTPInvoker struct {
	url string
	cli *http.Client
}

// NewHTTPInvoker returns HTTPInvoker instance.
func NewHTTPInvoker(rawurl string, dur time.Duration) (*HTTPInvoker, error) {
	if _, err := url.Parse(rawurl); err != nil {
		return nil, err
	}
	return &HTTPInvoker{
		url: rawurl,
		cli: &http.Client{
			Timeout: dur,
		},
	}, nil
}

// Invoke run http request to assigned URL.
func (ivk *HTTPInvoker) Invoke(ctx context.Context, q Message) error {
	buf := bytes.NewBuffer([]byte(q.Payload))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ivk.url, buf)
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/json")
	resp, err := ivk.cli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch s := resp.StatusCode; {
	case s >= http.StatusInternalServerError:
		b, _ := io.ReadAll(resp.Body)
		logger.Info("response is failure status",
			NewField("status_code", resp.StatusCode),
			NewField("body", string(b)))
		return fmt.Errorf("failure response: %d", s)
	case s >= http.StatusMultipleChoices:
		b, _ := io.ReadAll(resp.Body)
		logger.Info("response is not ok status",
			NewField("status_code", resp.StatusCode),
			NewField("body", string(b)))
	}
	return nil
}
