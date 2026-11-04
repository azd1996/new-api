package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// requestKeyRenameState holds the client's original body and parsed request
// while a channel's key-renamed copy is installed, so channel selection and
// every retry attempt see the untouched client request and rules never leak
// across channels.
type requestKeyRenameState struct {
	originalBody    common.BodyStorage
	originalRequest dto.Request
	// Retain stream changes across restores to guard later channel attempts.
	streamModeChanged bool
}

// restore puts the client's original body and request back and closes the
// renamed copy. It is a no-op when no rename is installed.
func (s *requestKeyRenameState) restore(c *gin.Context, info *relaycommon.RelayInfo) {
	if s.originalBody == nil {
		return
	}
	if installed, ok := c.Get(common.KeyBodyStorage); ok {
		if renamed, ok := installed.(common.BodyStorage); ok && renamed != s.originalBody {
			_ = renamed.Close()
		}
	}
	c.Set(common.KeyBodyStorage, s.originalBody)
	info.SetRequest(c, s.originalRequest)
	info.RequestKeyRenameAudit = nil
	s.originalBody = nil
	s.originalRequest = nil
}

// apply installs the selected channel's request_key_rename result when it
// changes the JSON body, and re-parses info.Request from it. Rename runs
// before DTO parsing because keys unknown to the DTO are dropped at parse
// time and cannot be recovered by the later param override.
func (s *requestKeyRenameState) apply(c *gin.Context, info *relaycommon.RelayInfo, format types.RelayFormat) *types.NewAPIError {
	if format == types.RelayFormatOpenAIRealtime || !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		return nil
	}
	setting, _ := common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
	rules := service.ResolveRequestKeyRenameRules(setting, info.OriginModelName)
	if len(rules) == 0 {
		return nil
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		// The relay loop reads the body right after this and reports the error.
		return nil
	}
	data, err := storage.Bytes()
	if err != nil {
		return nil
	}
	renamed, audit, err := relaycommon.RenameRequestKeys(data, rules)
	if err != nil {
		logger.LogWarn(c, common.LogText("request key rename skipped: %s", err.Error()))
		return nil
	}
	if len(audit) == 0 {
		return nil
	}

	renamedStorage, err := common.CreateBodyStorage(renamed)
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	c.Set(common.KeyBodyStorage, renamedStorage)
	request, err := helper.GetAndValidateRequest(c, format)
	if err != nil {
		c.Set(common.KeyBodyStorage, storage)
		_ = renamedStorage.Close()
		return types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithStatusCode(http.StatusBadRequest))
	}
	s.originalBody = storage
	s.originalRequest = info.Request
	wasStream := info.IsStream
	info.SetRequest(c, request)
	s.streamModeChanged = s.streamModeChanged || info.IsStream != wasStream
	info.RequestKeyRenameAudit = audit
	logger.LogDebug(c, "request key rename applied for channel #%d", common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	return nil
}
