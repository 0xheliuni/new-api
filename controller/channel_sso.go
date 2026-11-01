package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/channelsso"
	"github.com/gin-gonic/gin"
)

// channelSSOMessageErrors are flow errors whose text is a fixed English web
// console message key.
var channelSSOMessageErrors = []error{
	channelsso.ErrDisabled,
	channelsso.ErrNotConfigured,
	channelsso.ErrChannelNotFound,
	channelsso.ErrSessionForbidden,
	channelsso.ErrCredentialChanged,
	channelsso.ErrInvalidBaseURL,
	channelsso.ErrInvalidEntryPath,
	channelsso.ErrConfigIncomplete,
	channelsso.ErrUpstreamNotHTTPS,
	channelsso.ErrSiteUnreachable,
	channelsso.ErrNotNewAPI,
	channelsso.ErrChallengeRequired,
	channelsso.ErrSessionInvalid,
}

// respondChannelSSOError maps a Channel SSO error to a web console response
// without exposing internal details: fixed errors become message keys, an
// upstream-reported message is relayed verbatim (it is not a key), and
// anything else is logged and answered generically.
func respondChannelSSOError(c *gin.Context, err error) {
	var upstreamErr *channelsso.UpstreamError
	if errors.As(err, &upstreamErr) {
		common.ApiErrorMsg(c, upstreamErr.Message)
		return
	}
	if errors.Is(err, channelsso.ErrSessionNotFound) || errors.Is(err, channelsso.ErrSessionAlreadyActive) {
		common.ApiErrorT(c, "SSO session expired; please start again")
		return
	}
	for _, known := range channelSSOMessageErrors {
		if errors.Is(err, known) {
			common.ApiErrorT(c, known.Error())
			return
		}
	}
	common.SysError(common.LogText("channel sso request failed: %v", err))
	common.ApiErrorT(c, "Please try again later.")
}

// channelSSOChannelID gates the request on the feature being enabled and
// parses the :id channel parameter, writing the error response itself.
func channelSSOChannelID(c *gin.Context) (int, bool) {
	if err := channelsso.Enabled(); err != nil {
		respondChannelSSOError(c, err)
		return 0, false
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid channel ID")
		return 0, false
	}
	return id, true
}

func GetChannelSSOConfig(c *gin.Context) {
	id, ok := channelSSOChannelID(c)
	if !ok {
		return
	}
	view, err := channelsso.GetConfig(id)
	if err != nil {
		respondChannelSSOError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func SaveChannelSSOConfig(c *gin.Context) {
	id, ok := channelSSOChannelID(c)
	if !ok {
		return
	}
	var input channelsso.SaveConfigInput
	if err := c.ShouldBindJSON(&input); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := channelsso.SaveConfig(c.Request.Context(), id, input); err != nil {
		respondChannelSSOError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func CheckChannelSSO(c *gin.Context) {
	id, ok := channelSSOChannelID(c)
	if !ok {
		return
	}
	result, err := channelsso.Check(c.Request.Context(), id, c.GetInt("id"))
	if err != nil {
		respondChannelSSOError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func ValidateChannelSSO(c *gin.Context) {
	if err := channelsso.Enabled(); err != nil {
		respondChannelSSOError(c, err)
		return
	}
	user, err := channelsso.Validate(c.Request.Context(), c.Param("sid"), c.GetInt("id"))
	if err != nil {
		respondChannelSSOError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"username": user.Username, "role": user.Role})
}

func OpenChannelSSO(c *gin.Context) {
	if err := channelsso.Enabled(); err != nil {
		respondChannelSSOError(c, err)
		return
	}
	enterURL, session, err := channelsso.OpenSession(c.Request.Context(), c.Param("sid"), c.GetInt("id"))
	if err != nil {
		respondChannelSSOError(c, err)
		return
	}
	// The entry URL carries the one-time ticket, so it is never audited.
	const action = "channel.sso_open"
	model.RecordAuditLog(c, model.AuditLog{
		UserId:    c.GetInt("id"),
		Username:  c.GetString("username"),
		ActorRole: c.GetInt("role"),
		Category:  model.AuditCategoryOperation,
		Action:    action,
		Content:   "Opened upstream console via channel SSO",
		Status:    http.StatusOK,
		Success:   true,
		Other: model.AuditOther{
			Op: &model.AuditOperation{Action: action, Params: model.AuditFields{
				"channel_id":        session.ChannelID,
				"upstream_base_url": session.BaseURL,
				"upstream_username": session.User.Username,
				"upstream_role":     session.User.Role,
			}},
			AdminInfo: auditOperatorInfo(c),
		},
	})
	markAuditLogged(c)
	common.ApiSuccess(c, gin.H{"enter_url": enterURL})
}
