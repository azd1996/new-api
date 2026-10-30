package common

import (
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ResponseFilterRules returns the channel's ResponseFilter rules, or nil when the
// relay has no channel metadata yet (early failure paths, handler unit tests).
func (info *RelayInfo) ResponseFilterRules() []string {
	if info == nil || info.ChannelMeta == nil {
		return nil
	}
	return info.ChannelSetting.ResponseFilter
}

// FilterResponseJSON removes fields configured by a channel's ResponseFilter from
// a JSON response body before it reaches the client. Each rule is either an exact
// gjson/sjson path (when it contains "." or "#"), deleted via sjson, or a bare key
// name, deleted recursively at every nesting level. A nil/empty rule set or a
// non-JSON body is returned unchanged, so the stream [DONE] sentinel and SSE event
// lines pass through untouched. Errors are swallowed: filtering must never break
// the relay, so at worst the body is left as-is.
func FilterResponseJSON(body []byte, rules []string) []byte {
	if len(body) == 0 || len(rules) == 0 {
		return body
	}
	if !gjson.ValidBytes(body) {
		return body
	}

	keyRules := make(map[string]struct{})
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		if strings.ContainsAny(rule, ".#") {
			if next, err := sjson.DeleteBytes(body, rule); err == nil {
				body = next
			}
			continue
		}
		keyRules[rule] = struct{}{}
	}

	if len(keyRules) == 0 {
		return body
	}

	var parsed any
	if err := common.Unmarshal(body, &parsed); err != nil {
		return body
	}
	parsed = deleteJSONKeysRecursive(parsed, keyRules)
	filtered, err := common.Marshal(parsed)
	if err != nil {
		return body
	}
	return filtered
}

// deleteJSONKeysRecursive removes every occurrence of the named keys from a decoded
// JSON value, descending into nested objects and arrays. Maps are pruned in place;
// array elements are processed one by one.
func deleteJSONKeysRecursive(value any, keys map[string]struct{}) any {
	switch typed := value.(type) {
	case map[string]any:
		for key := range typed {
			if _, drop := keys[key]; drop {
				delete(typed, key)
				continue
			}
			typed[key] = deleteJSONKeysRecursive(typed[key], keys)
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = deleteJSONKeysRecursive(typed[i], keys)
		}
		return typed
	default:
		return value
	}
}
