package operation_setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

const (
	// AIAssistantOptionPrefix is the option key prefix for AI assistant settings.
	AIAssistantOptionPrefix = "ai_assistant_setting."

	DefaultAIAssistantTimeoutSeconds = 60
	MinAIAssistantTimeoutSeconds     = 5
	MaxAIAssistantTimeoutSeconds     = 300

	DefaultAIAssistantMaxTokens = 2048
	MaxAIAssistantMaxTokens     = 32768

	MinAIAssistantTemperature = 0
	MaxAIAssistantTemperature = 2
)

// AIAssistantSetting configures the AI assistant channel group. The assistant
// runs through the ordinary relay stack: requests are attributed to the group
// configured here, so special assistant channels can be managed like any other.
type AIAssistantSetting struct {
	Enabled        bool    `json:"enabled"`
	Group          string  `json:"group"`
	DefaultModel   string  `json:"default_model"`
	TimeoutSeconds int     `json:"timeout_seconds"`
	MaxTokens      int     `json:"max_tokens"`
	Temperature    float64 `json:"temperature"`
}

var aiAssistantSetting = AIAssistantSetting{
	TimeoutSeconds: DefaultAIAssistantTimeoutSeconds,
	MaxTokens:      DefaultAIAssistantMaxTokens,
	Temperature:    0.7,
}

func init() {
	config.GlobalConfig.Register("ai_assistant_setting", &aiAssistantSetting)
}

func GetAIAssistantSetting() AIAssistantSetting {
	return normalizeAIAssistantSetting(aiAssistantSetting)
}

// AIAssistantSettingFromMap applies raw option values (keyed without the
// ai_assistant_setting. prefix) on top of base and returns a normalized
// setting. It lets callers read the persisted configuration directly instead
// of relying on the node-local in-memory copy.
func AIAssistantSettingFromMap(base AIAssistantSetting, values map[string]string) AIAssistantSetting {
	_ = config.UpdateConfigFromMap(&base, values)
	return normalizeAIAssistantSetting(base)
}

func normalizeAIAssistantSetting(setting AIAssistantSetting) AIAssistantSetting {
	setting.Group = strings.TrimSpace(setting.Group)
	setting.DefaultModel = strings.TrimSpace(setting.DefaultModel)
	if setting.TimeoutSeconds < MinAIAssistantTimeoutSeconds {
		setting.TimeoutSeconds = MinAIAssistantTimeoutSeconds
	} else if setting.TimeoutSeconds > MaxAIAssistantTimeoutSeconds {
		setting.TimeoutSeconds = MaxAIAssistantTimeoutSeconds
	}
	if setting.MaxTokens <= 0 {
		setting.MaxTokens = DefaultAIAssistantMaxTokens
	} else if setting.MaxTokens > MaxAIAssistantMaxTokens {
		setting.MaxTokens = MaxAIAssistantMaxTokens
	}
	if setting.Temperature < MinAIAssistantTemperature {
		setting.Temperature = MinAIAssistantTemperature
	} else if setting.Temperature > MaxAIAssistantTemperature {
		setting.Temperature = MaxAIAssistantTemperature
	}
	return setting
}

// AIAssistantGroupEnabled reports whether the group is the configured,
// enabled AI assistant group. The playground route and model listing use it to
// grant access to the assistant group without adding it to every user.
func AIAssistantGroupEnabled(group string) bool {
	setting := GetAIAssistantSetting()
	return setting.Enabled && setting.Group != "" && setting.Group == group
}

// ValidateAIAssistantOption validates a single persisted AI assistant option.
func ValidateAIAssistantOption(key, value string) error {
	switch key {
	case AIAssistantOptionPrefix + "enabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("AI assistant enabled must be a boolean")
		}
	case AIAssistantOptionPrefix + "group":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("AI assistant group cannot be empty")
		}
		if strings.ContainsAny(value, " \t") {
			return fmt.Errorf("AI assistant group cannot contain whitespace")
		}
	case AIAssistantOptionPrefix + "default_model":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("AI assistant default model cannot be empty")
		}
	case AIAssistantOptionPrefix + "timeout_seconds":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < MinAIAssistantTimeoutSeconds || n > MaxAIAssistantTimeoutSeconds {
			return fmt.Errorf("AI assistant timeout must be between %d and %d seconds", MinAIAssistantTimeoutSeconds, MaxAIAssistantTimeoutSeconds)
		}
	case AIAssistantOptionPrefix + "max_tokens":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n <= 0 || n > MaxAIAssistantMaxTokens {
			return fmt.Errorf("AI assistant max tokens must be between 1 and %d", MaxAIAssistantMaxTokens)
		}
	case AIAssistantOptionPrefix + "temperature":
		f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || f < MinAIAssistantTemperature || f > MaxAIAssistantTemperature {
			return fmt.Errorf("AI assistant temperature must be between %v and %v", MinAIAssistantTemperature, MaxAIAssistantTemperature)
		}
	default:
		return nil
	}
	return nil
}
