package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// This file implements the read-only VictoriaMetrics / Prometheus query path for
// the auto-scheduling feature. It substitutes `__PLACEHOLDER__` tokens in a rule
// template, forwards an instant query to the data source, and reduces the
// response to (label, value) rows keyed by the template's result label. It never
// evaluates PromQL itself and never influences routing.

// placeholderPattern matches tokens like __MODEL__ / __TIME_RANGE__.
var placeholderPattern = regexp.MustCompile(`__[A-Z0-9_]+__`)

// ScheduleQueryRow is one reduced result row: the value of the template's result
// label, the numeric sample value, and the full label set for reference.
type ScheduleQueryRow struct {
	Label  string            `json:"label"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels"`
}

// ScheduleQueryResult is what a preview returns to the admin UI.
type ScheduleQueryResult struct {
	ResolvedPromQL string             `json:"resolved_promql"`
	Rows           []ScheduleQueryRow `json:"rows"`
}

// ExtractSchedulePlaceholders returns the distinct placeholder names (without the
// surrounding underscores, e.g. "MODEL") found in a PromQL template.
func ExtractSchedulePlaceholders(promql string) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, tok := range placeholderPattern.FindAllString(promql, -1) {
		name := strings.Trim(tok, "_")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// resolveSchedulePromQL replaces every __NAME__ token for which params has a
// value. Returns the resolved query and the list of placeholder names still left
// unresolved (so the caller can reject the request with a clear error).
func resolveSchedulePromQL(promql string, params map[string]string) (string, []string) {
	resolved := promql
	for name, value := range params {
		resolved = strings.ReplaceAll(resolved, "__"+name+"__", value)
	}
	var missing []string
	for _, name := range ExtractSchedulePlaceholders(resolved) {
		missing = append(missing, name)
	}
	return resolved, missing
}

// promInstantResponse models the subset of the Prometheus/VictoriaMetrics
// instant-query response we consume.
type promInstantResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
}

// queryScheduleDatasource runs an instant query against the data source and
// returns rows reduced by resultLabel. resultLabel may be empty, in which case
// the row label falls back to the full metric signature.
func queryScheduleDatasource(ds *model.XScheduleDatasource, promql, resultLabel string) ([]ScheduleQueryRow, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(ds.BaseURL), "/") + "/api/v1/query"
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("数据源 base_url 无效，仅支持 http/https")
	}

	timeout := ds.TimeoutSeconds
	if timeout <= 0 {
		timeout = 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	form := url.Values{}
	form.Set("query", promql)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if ds.Username != "" || ds.Password != "" {
		req.SetBasicAuth(ds.Username, ds.Password)
	}

	resp, err := (&http.Client{Timeout: time.Duration(timeout) * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求数据源失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("数据源返回 %d: %s", resp.StatusCode, msg)
	}

	var parsedResp promInstantResponse
	// Bound the success-path body read too: an abnormal/compromised upstream
	// could otherwise stream an oversized response within the timeout window.
	if err := common.DecodeJson(io.LimitReader(resp.Body, 8<<20), &parsedResp); err != nil {
		return nil, fmt.Errorf("解析数据源响应失败: %w", err)
	}
	if parsedResp.Status != "success" {
		msg := parsedResp.Error
		if msg == "" {
			msg = parsedResp.ErrorType
		}
		return nil, fmt.Errorf("数据源查询失败: %s", msg)
	}

	rows := make([]ScheduleQueryRow, 0, len(parsedResp.Data.Result))
	for _, item := range parsedResp.Data.Result {
		var value float64
		if len(item.Value) == 2 {
			var raw string
			if err := common.Unmarshal(item.Value[1], &raw); err == nil {
				value, _ = strconv.ParseFloat(raw, 64)
			}
		}
		label := ""
		if resultLabel != "" {
			label = item.Metric[resultLabel]
		}
		rows = append(rows, ScheduleQueryRow{Label: label, Value: value, Labels: item.Metric})
	}
	return rows, nil
}

// PreviewSchedulePromQL resolves placeholders and runs the query, returning the
// reduced rows for the admin preview. It rejects a query that still has
// unresolved placeholders so the user gets an actionable error.
func PreviewSchedulePromQL(ds *model.XScheduleDatasource, promql, resultLabel string, params map[string]string) (*ScheduleQueryResult, error) {
	resolved, missing := resolveSchedulePromQL(promql, params)
	if len(missing) > 0 {
		return nil, fmt.Errorf("存在未填写的占位符: %s", strings.Join(missing, ", "))
	}
	rows, err := queryScheduleDatasource(ds, resolved, resultLabel)
	if err != nil {
		return nil, err
	}
	return &ScheduleQueryResult{ResolvedPromQL: resolved, Rows: rows}, nil
}

// TestScheduleDatasource runs a trivial query to verify connectivity and auth.
func TestScheduleDatasource(ds *model.XScheduleDatasource) error {
	_, err := queryScheduleDatasource(ds, "vector(1)", "")
	return err
}
