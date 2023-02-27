package rest

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/pkg/errors"
	log "github.com/treeforest/logger"
)

type Client struct {
	client  *http.Client
	baseUrl string
}

func NewClient(baseUrl string) *Client {
	c := &Client{
		client:  &http.Client{},
		baseUrl: baseUrl,
	}
	return c
}

func (c *Client) Proxy(method, path string, header http.Header, body []byte) (
	respHeader http.Header, statusCode int, respBody []byte, err error) {

	url := fmt.Sprintf("%s%s", c.baseUrl, path)

	// 日志输出
	start := time.Now()
	defer func() {
		end := time.Now()
		log.Infof("[REST] METHOD:%s | URL:%s | CODE:%d | TIME:%d", method, url, statusCode, end.Sub(start).Milliseconds())
	}()

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, errors.WithStack(err)
	}

	req.Header = header

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, 0, nil, errors.WithStack(err)
	}

	defer resp.Body.Close()

	respBody, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, nil, err
	}

	return resp.Header, resp.StatusCode, respBody, nil
}
