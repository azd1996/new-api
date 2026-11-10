package ruliu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	defaultTimeout    = 10 * time.Second
	maxResponseBytes  = 64 << 10
	maxErrorBodyBytes = 512
)

type AlertParams struct {
	AlertToID []int    `json:"-"`
	AlertURL  string   `json:"-"`
	Msg       string   `json:"-"`
	Link      string   `json:"-"`
	AtUserIDs []string `json:"-"`
}

type Client struct {
	httpClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{httpClient: httpClient}
}

func (client *Client) Send(ctx context.Context, params AlertParams) error {
	if strings.TrimSpace(params.AlertURL) == "" {
		return errors.New("ruliu alert URL is empty")
	}
	if len(params.AlertToID) == 0 {
		return errors.New("ruliu alert recipient list is empty")
	}
	if strings.TrimSpace(params.Msg) == "" {
		return errors.New("ruliu alert message is empty")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	postData := map[string]any{
		"message": map[string]any{
			"header": map[string]any{
				"toid": params.AlertToID,
			},
			"body": buildBody(params),
		},
	}
	jsonData, err := common.Marshal(postData)
	if err != nil {
		return fmt.Errorf("marshal ruliu alert request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, params.AlertURL, strings.NewReader(string(jsonData)))
	if err != nil {
		return fmt.Errorf("create ruliu alert request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("send ruliu alert request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read ruliu alert response: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("ruliu alert returned HTTP %d: %s", response.StatusCode, truncateErrorBody(body))
	}

	var result responseBody
	if err := common.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode ruliu alert response: %w", err)
	}
	if result.ErrCode != 0 || len(result.Data.Fail) > 0 {
		return fmt.Errorf("ruliu alert failed: %s", truncateErrorBody(body))
	}
	return nil
}

type responseBody struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
	Data    struct {
		Fail map[string]string `json:"fail"`
	} `json:"data"`
}

func buildBody(params AlertParams) []map[string]any {
	body := []map[string]any{
		{
			"content": params.Msg,
			"type":    "TEXT",
		},
	}
	if len(params.AtUserIDs) > 0 {
		body = append(body, map[string]any{
			"type":      "AT",
			"atuserids": params.AtUserIDs,
		})
	}
	body = append(body, map[string]any{
		"content": "\n",
		"type":    "TEXT",
	})
	if params.Link != "" {
		body = append(body, map[string]any{
			"href": params.Link,
			"type": "LINK",
		})
	}
	return body
}

func truncateErrorBody(body []byte) string {
	const suffix = "..."
	if len(body) <= maxErrorBodyBytes {
		return string(body)
	}
	return string(body[:maxErrorBodyBytes-len(suffix)]) + suffix
}
