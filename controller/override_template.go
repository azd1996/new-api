package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type overrideTemplateOption struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// GetOverrideTemplateOptions returns the names of the shared parameter-override
// and retry-override templates, so the channel editor can offer them in a
// multi-select. Only names/descriptions are exposed (not the rule content),
// and it is reachable by channel admins (unlike the root-only option API where
// the templates are edited).
func GetOverrideTemplateOptions(c *gin.Context) {
	setting := operation_setting.GetOverrideTemplateSetting()
	toOptions := func(items []operation_setting.OverrideTemplateItem) []overrideTemplateOption {
		out := make([]overrideTemplateOption, 0, len(items))
		for _, item := range items {
			name := strings.TrimSpace(item.Name)
			if name == "" {
				continue
			}
			out = append(out, overrideTemplateOption{Name: name, Description: item.Description})
		}
		return out
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"param": toOptions(setting.ParamTemplates),
			"retry": toOptions(setting.RetryTemplates),
		},
	})
}
