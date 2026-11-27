package common

import "github.com/QuantumNous/new-api/relaykit/dto"

// ClaudeInputUsage is the input side of one usage report in a Claude stream.
// CacheCreation5m and CacheCreation1h hold the cache_creation breakdown, which
// a report carries only when HasCacheCreation is set.
type ClaudeInputUsage struct {
	InputTokens              int  `json:"input_tokens"`
	CacheCreationInputTokens int  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int  `json:"cache_read_input_tokens"`
	HasCacheCreation         bool `json:"has_cache_creation,omitempty"`
	CacheCreation5m          int  `json:"ephemeral_5m_input_tokens,omitempty"`
	CacheCreation1h          int  `json:"ephemeral_1h_input_tokens,omitempty"`
}

// StreamUsageCheck keeps the input usage a Claude stream reported in
// message_start and in message_delta. Input tokens are final once the stream
// starts, so the two reports must agree. It is diagnostic only: it must never
// change billing, the usage sent to the client, or the request outcome.
type StreamUsageCheck struct {
	Start ClaudeInputUsage
	Delta ClaudeInputUsage

	startSeen bool
	deltaSeen bool
}

func claudeInputUsage(usage *dto.ClaudeUsage) ClaudeInputUsage {
	if usage == nil {
		return ClaudeInputUsage{}
	}
	return ClaudeInputUsage{
		InputTokens:              usage.InputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		HasCacheCreation:         usage.CacheCreation != nil,
		CacheCreation5m:          usage.GetCacheCreation5mTokens(),
		CacheCreation1h:          usage.GetCacheCreation1hTokens(),
	}
}

// ObserveClaudeStreamUsage records the input usage of a message_start or
// message_delta event. Only observe upstream events, never ones rebuilt by a
// response converter. A later message_delta replaces an earlier one because
// its usage is cumulative.
func (info *RelayInfo) ObserveClaudeStreamUsage(response *dto.ClaudeResponse) {
	if info == nil || response == nil {
		return
	}
	if response.Type != "message_start" && response.Type != "message_delta" {
		return
	}
	if info.StreamUsageCheck == nil {
		info.StreamUsageCheck = &StreamUsageCheck{}
	}
	check := info.StreamUsageCheck
	if response.Type == "message_delta" {
		check.Delta = claudeInputUsage(response.Usage)
		check.deltaSeen = true
		return
	}
	if check.startSeen {
		return
	}
	if response.Message != nil {
		check.Start = claudeInputUsage(response.Message.Usage)
	}
	check.startSeen = true
}

// Checked reports whether the stream delivered both message_start and
// message_delta, so the two reports can be compared.
func (c *StreamUsageCheck) Checked() bool {
	return c != nil && c.startSeen && c.deltaSeen
}

// Anomalies compares the two reports field by field and returns the fields
// whose message_delta value differs from message_start (tampered) and the
// fields message_start reported but message_delta left out or zeroed
// (unreported). Any difference counts, in either direction. The cache_creation
// breakdown is compared only when message_delta carries that object.
func (c *StreamUsageCheck) Anomalies() (tampered, unreported []string) {
	if !c.Checked() {
		return nil, nil
	}
	type usageField struct {
		name         string
		start, delta int
	}
	fields := []usageField{
		{"input_tokens", c.Start.InputTokens, c.Delta.InputTokens},
		{"cache_creation_input_tokens", c.Start.CacheCreationInputTokens, c.Delta.CacheCreationInputTokens},
		{"cache_read_input_tokens", c.Start.CacheReadInputTokens, c.Delta.CacheReadInputTokens},
	}
	if c.Delta.HasCacheCreation {
		fields = append(fields,
			usageField{"ephemeral_5m_input_tokens", c.Start.CacheCreation5m, c.Delta.CacheCreation5m},
			usageField{"ephemeral_1h_input_tokens", c.Start.CacheCreation1h, c.Delta.CacheCreation1h},
		)
	}
	for _, field := range fields {
		switch {
		case field.start == field.delta:
		case field.delta == 0:
			unreported = append(unreported, field.name)
		default:
			tampered = append(tampered, field.name)
		}
	}
	return tampered, unreported
}
