package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Control-plane passthrough requests are signed with the selected channel's
// BytePlus credentials and signing overrides, and go out through the channel's
// proxy. The signing settings exist only on the fork's host settings type and
// the proxy only on the relaykit settings type, so the credentials must read
// each from what channel selection stored; otherwise every signed request goes
// out with empty keys or bypasses the proxy.
func TestVolcPassthroughCredentialsCarryTheSelectedChannelSigningSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/volc/?Action=ListAssets&Version=2024-01-01", nil)
	baseURL := "https://ark.ap-southeast.bytepluses.com"
	setting := `{"proxy":"http://proxy.example:8080"}`
	channel := &model.Channel{
		Id:      66,
		Type:    constant.ChannelTypeVolcPassthrough,
		Key:     "ark-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
		Setting: &setting,
		OtherSettings: `{"byteplus_access_key":"AK-test","byteplus_secret_key":"SK-test",` +
			`"byteplus_project_name":"project-a","volc_sign_region":"cn-shanghai"}`,
	}
	require.Nil(t, middleware.SetupContextForSelectedChannel(c, channel, ""))

	cred, err := buildPassthroughCredentials(c)

	require.NoError(t, err)
	assert.Equal(t, baseURL, cred.BaseURL)
	assert.Equal(t, "ark-key", cred.APIKey)
	assert.Equal(t, "http://proxy.example:8080", cred.Proxy)
	assert.Equal(t, "AK-test", cred.AccessKey)
	assert.Equal(t, "SK-test", cred.SecretKey)
	assert.Equal(t, "project-a", cred.OtherSettings.BytePlusProjectName)
	assert.Equal(t, "cn-shanghai", cred.OtherSettings.ResolveVolcSignRegion(cred.BaseURL))
}
