package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// MergeParamOverrideTemplates folds the named parameter-override templates
// referenced by a channel onto the channel's own param-override, preserving the
// selection order with the channel's own operations LAST (template ops run
// first, then the channel can do final per-channel tweaks on top). Reuses
// mergeChannelOverride so the merge semantics match channel-affinity exactly.
//
// Missing or unparseable template references are skipped silently (never fatal),
// consistent with how a malformed override rule is skipped elsewhere.
func MergeParamOverrideTemplates(refs []string, base map[string]interface{}) map[string]interface{} {
	if len(refs) == 0 {
		return base
	}
	byName := paramOverrideTemplatesByName()
	if len(byName) == 0 {
		return base
	}
	// Fold from the last reference to the first so the final operations order is
	// [ref0, ref1, ..., channelOwn]: each merge prepends its template ops before
	// the accumulated base.
	out := base
	for i := len(refs) - 1; i >= 0; i-- {
		name := strings.TrimSpace(refs[i])
		if name == "" {
			continue
		}
		tpl, ok := byName[name]
		if !ok {
			continue
		}
		out = mergeChannelOverride(out, tpl)
	}
	return out
}

// ResolveRetryOverrideTemplates returns the retry-override rules contributed by
// the named templates a channel references, concatenated in selection order.
// Callers prepend these before the channel's own rules. Missing or unparseable
// references are skipped silently.
func ResolveRetryOverrideTemplates(refs []string) []dto.RetryRule {
	if len(refs) == 0 {
		return nil
	}
	byName := retryOverrideTemplatesByName()
	if len(byName) == 0 {
		return nil
	}
	out := make([]dto.RetryRule, 0, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref)
		if name == "" {
			continue
		}
		if rules, ok := byName[name]; ok {
			out = append(out, rules...)
		}
	}
	return out
}

func paramOverrideTemplatesByName() map[string]map[string]interface{} {
	setting := operation_setting.GetOverrideTemplateSetting()
	byName := make(map[string]map[string]interface{}, len(setting.ParamTemplates))
	for _, item := range setting.ParamTemplates {
		name := strings.TrimSpace(item.Name)
		if name == "" || len(item.Content) == 0 {
			continue
		}
		if obj := paramTemplateContentObject(item.Content); obj != nil {
			byName[name] = obj
		}
	}
	return byName
}

func retryOverrideTemplatesByName() map[string][]dto.RetryRule {
	setting := operation_setting.GetOverrideTemplateSetting()
	byName := make(map[string][]dto.RetryRule, len(setting.RetryTemplates))
	for _, item := range setting.RetryTemplates {
		name := strings.TrimSpace(item.Name)
		if name == "" || len(item.Content) == 0 {
			continue
		}
		var rules []dto.RetryRule
		if err := common.Unmarshal(item.Content, &rules); err != nil {
			continue
		}
		byName[name] = rules
	}
	return byName
}

// paramTemplateContentObject normalizes a param template's stored content into a
// param-override object. It accepts either the object form
// (`{"operations":[...]}`) or a bare operations array (wrapped into that object).
func paramTemplateContentObject(content []byte) map[string]interface{} {
	var obj map[string]interface{}
	if err := common.Unmarshal(content, &obj); err == nil && obj != nil {
		return obj
	}
	var arr []interface{}
	if err := common.Unmarshal(content, &arr); err == nil {
		return map[string]interface{}{"operations": arr}
	}
	return nil
}
