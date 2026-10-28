package operation_setting

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/setting/config"
)

// OverrideTemplateItem is one named, reusable override rule-set. The Content is
// kept as raw JSON so this package stays free of relay/dto imports:
//   - param templates: a param-override object `{"operations":[...]}` (an
//     operations array is also accepted and treated as that object).
//   - retry templates: a JSON array of retry-override rules (dto.RetryRule).
//
// Parsing into concrete shapes happens in the service layer at resolution time.
type OverrideTemplateItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Content     json.RawMessage `json:"content"`
}

// OverrideTemplateSetting is the named-template library referenced by channels.
// Templates are strictly separated by kind: a channel's parameter-override
// multi-select only lists ParamTemplates, its retry-override multi-select only
// lists RetryTemplates. Stored as a single options blob (two JSON array keys),
// edited via the generic option API, and resolved at request time so editing a
// template takes effect for every referencing channel without re-saving them.
type OverrideTemplateSetting struct {
	ParamTemplates []OverrideTemplateItem `json:"param_templates"`
	RetryTemplates []OverrideTemplateItem `json:"retry_templates"`
}

var overrideTemplateSetting = OverrideTemplateSetting{
	ParamTemplates: []OverrideTemplateItem{},
	RetryTemplates: []OverrideTemplateItem{},
}

func init() {
	config.GlobalConfig.Register("override_template_setting", &overrideTemplateSetting)
}

func GetOverrideTemplateSetting() *OverrideTemplateSetting {
	return &overrideTemplateSetting
}
