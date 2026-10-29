package controller

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Both settings pages load the options API, so a saved content provenance key
// must never appear in it, whatever the setting field is named.
func TestGetOptionsNeverReturnsTheContentProvenanceKey(t *testing.T) {
	const savedKey = "sk-provenance-secret"
	setting := operation_setting.GetImageDetectionSetting()
	previousSetting := *setting
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		*setting = previousSetting
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})

	setting.ProvenanceAPIKey = savedKey
	// InitOptionMap fills the option map from the registered settings this way.
	common.OptionMapRWMutex.Lock()
	maps.Copy(common.OptionMap, config.GlobalConfig.ExportAllConfigs())
	common.OptionMapRWMutex.Unlock()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/option/", nil)
	GetOptions(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Contains(t, body, "image_detection_setting.check_synth_id")
	assert.NotContains(t, body, savedKey)
}
