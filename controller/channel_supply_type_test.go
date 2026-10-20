package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelSupplyTypeFilter 覆盖供应方式标签在列表与搜索接口上的筛选。
// TEST_CHANNEL_SQL_DSN 为空时走 SQLite，设置后可对 MySQL/PostgreSQL 复跑。
func TestChannelSupplyTypeFilter(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousCache, previousRedis, previousSQLite := common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled, common.SQLitePath = previousMaster, previousCache, previousRedis, previousSQLite
	})
	t.Setenv("SQL_DSN", os.Getenv("TEST_CHANNEL_SQL_DSN"))
	t.Setenv("LOG_SQL_DSN", "")
	common.IsMasterNode, common.MemoryCacheEnabled, common.RedisEnabled = false, false, false
	common.SQLitePath = filepath.Join(t.TempDir(), "channel.db")
	require.NoError(t, model.InitDB())
	database := model.DB
	sqlDB, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	model.LOG_DB = database
	common.SetLogDatabaseType(common.MainDatabaseType())
	require.NoError(t, database.AutoMigrate(&model.Channel{}, &model.Ability{}))

	insert := func(name, supplyType string) *model.Channel {
		ch := &model.Channel{Name: name, Type: 1, Key: name + "-key", Status: common.ChannelStatusEnabled,
			Models: "supply-model", Group: "default", SupplyType: supplyType}
		require.NoError(t, ch.Insert())
		t.Cleanup(func() { require.NoError(t, ch.Delete()) })
		return ch
	}
	keyCh := insert("supply-key", model.ChannelSupplyTypeKey)
	poolCh := insert("supply-pool", model.ChannelSupplyTypePool)
	// 列加上之前写入的旧行：supply_type 为空串，按 key 处理。
	legacyCh := insert("supply-legacy", model.ChannelSupplyTypeKey)
	require.NoError(t, database.Model(&model.Channel{}).Where("id = ?", legacyCh.Id).
		UpdateColumn("supply_type", "").Error)

	listIds := func(handler gin.HandlerFunc, url string) []int {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, url, nil)
		handler(c)
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Items []model.Channel `json:"items"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
		require.True(t, result.Success, recorder.Body.String())
		ids := make([]int, 0, len(result.Data.Items))
		for _, item := range result.Data.Items {
			ids = append(ids, item.Id)
		}
		return ids
	}

	for _, tc := range []struct {
		name       string
		supplyType string
		want       []int
	}{
		{name: "pool only", supplyType: "pool", want: []int{poolCh.Id}},
		{name: "key includes legacy empty rows", supplyType: "key", want: []int{keyCh.Id, legacyCh.Id}},
		{name: "no filter returns all", supplyType: "", want: []int{keyCh.Id, poolCh.Id, legacyCh.Id}},
		{name: "unknown value is ignored", supplyType: "bogus", want: []int{keyCh.Id, poolCh.Id, legacyCh.Id}},
	} {
		t.Run("list/"+tc.name, func(t *testing.T) {
			got := listIds(GetAllChannels, "/api/channel/?p=1&page_size=50&supply_type="+tc.supplyType)
			assert.ElementsMatch(t, tc.want, got)
		})
		t.Run("search/"+tc.name, func(t *testing.T) {
			got := listIds(SearchChannels, "/api/channel/search?keyword=supply-&p=1&page_size=50&supply_type="+tc.supplyType)
			assert.ElementsMatch(t, tc.want, got)
		})
	}

	// 批量设置放在筛选用例之后：它会改写上面三条渠道的取值。
	batchSet := func(body string) bool {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/channel/batch/supply_type", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		BatchSetChannelSupplyType(c)
		var result struct {
			Success bool `json:"success"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
		return result.Success
	}
	supplyTypeOf := func(id int) string {
		ch, err := model.GetChannelById(id, false)
		require.NoError(t, err)
		return ch.SupplyType
	}

	t.Run("batch/sets only the selected channels", func(t *testing.T) {
		body := fmt.Sprintf(`{"ids":[%d,%d],"supply_type":"pool"}`, keyCh.Id, legacyCh.Id)
		require.True(t, batchSet(body))
		assert.Equal(t, model.ChannelSupplyTypePool, supplyTypeOf(keyCh.Id))
		assert.Equal(t, model.ChannelSupplyTypePool, supplyTypeOf(legacyCh.Id))
		assert.Equal(t, model.ChannelSupplyTypePool, supplyTypeOf(poolCh.Id))

		body = fmt.Sprintf(`{"ids":[%d],"supply_type":"key"}`, poolCh.Id)
		require.True(t, batchSet(body))
		assert.Equal(t, model.ChannelSupplyTypeKey, supplyTypeOf(poolCh.Id))
		assert.Equal(t, model.ChannelSupplyTypePool, supplyTypeOf(keyCh.Id))
	})

	t.Run("batch/rejects invalid requests without writing", func(t *testing.T) {
		for _, body := range []string{
			fmt.Sprintf(`{"ids":[%d],"supply_type":"vendor"}`, keyCh.Id),
			fmt.Sprintf(`{"ids":[%d],"supply_type":""}`, keyCh.Id),
			`{"ids":[],"supply_type":"key"}`,
		} {
			assert.False(t, batchSet(body), body)
		}
		assert.Equal(t, model.ChannelSupplyTypePool, supplyTypeOf(keyCh.Id))
	})
}

func TestValidateChannelSupplyType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   string
		isAdd   bool
		want    string
		wantErr bool
	}{
		{name: "create defaults empty to key", input: "", isAdd: true, want: model.ChannelSupplyTypeKey},
		{name: "update keeps empty as unchanged", input: "", isAdd: false, want: ""},
		{name: "pool accepted", input: model.ChannelSupplyTypePool, isAdd: true, want: model.ChannelSupplyTypePool},
		{name: "unknown rejected", input: "vendor", isAdd: false, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := &model.Channel{Type: 1, Key: "sk-test", Models: "supply-model", SupplyType: tc.input}
			err := validateChannel(ch, tc.isAdd)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, ch.SupplyType)
		})
	}
}
