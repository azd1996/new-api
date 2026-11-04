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
//   - request key rename templates: a JSON array of request-body key rename
//     rules (dto.RequestKeyRenameRule).
//
// Parsing into concrete shapes happens in the service layer at resolution time.
type OverrideTemplateItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Content     json.RawMessage `json:"content"`
}

// OverrideTemplateSetting is the named-template library referenced by channels.
// Templates are strictly separated by kind: each channel multi-select
// (parameter override, retry override, request key rename) only lists the
// templates of its own kind. Stored as a single options blob (one JSON array
// key per kind),
// edited via the generic option API, and resolved at request time so editing a
// template takes effect for every referencing channel without re-saving them.
type OverrideTemplateSetting struct {
	ParamTemplates []OverrideTemplateItem `json:"param_templates"`
	RetryTemplates []OverrideTemplateItem `json:"retry_templates"`
	// RequestKeyRenameTemplates are shared request-body key rename rule sets.
	RequestKeyRenameTemplates []OverrideTemplateItem `json:"request_key_rename_templates"`
}

var overrideTemplateSetting = OverrideTemplateSetting{
	ParamTemplates:            []OverrideTemplateItem{},
	RetryTemplates:            []OverrideTemplateItem{},
	RequestKeyRenameTemplates: []OverrideTemplateItem{},
}

func init() {
	config.GlobalConfig.Register("override_template_setting", &overrideTemplateSetting)
}

func GetOverrideTemplateSetting() *OverrideTemplateSetting {
	return &overrideTemplateSetting
}
