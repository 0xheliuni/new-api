package controller

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// costVersionPostContext 组装一次 POST /api/cost/channels/:id/versions 的调用现场。
// 路由参数必须手工塞进 c.Params：CreateChannelCostVersion 从 c.Param("id") 取渠道。
func costVersionPostContext(channelID int, body string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST",
		"/api/cost/channels/"+strconv.Itoa(channelID)+"/versions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: strconv.Itoa(channelID)}}
	c.Set("id", 1) // 中间件已验证的登录用户，落到 CreatedBy
	return c, rec
}

type costVersionResponse struct {
	Success bool                     `json:"success"`
	Message string                   `json:"message"`
	Data    model.ChannelCostVersion `json:"data"`
}

func decodeCostVersionResponse(t *testing.T, rec *httptest.ResponseRecorder) costVersionResponse {
	t.Helper()
	var resp costVersionResponse
	if err := common.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response %q: %v", rec.Body.String(), err)
	}
	return resp
}

// seedCostVersionChannel 建一个渠道行：CreateChannelCostVersion 会拒绝孤儿版本。
func seedCostVersionChannel(t *testing.T, channelID int) {
	t.Helper()
	if err := model.DB.Exec(
		"INSERT INTO channels (id, name, `key`, status) VALUES (?, ?, ?, ?)",
		channelID, "upstream-"+strconv.Itoa(channelID), "sk-test", 1).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
}

// withUSDExchangeRate 临时改写服务端汇率，测完还原（包级变量，会串到别的测试）。
func withUSDExchangeRate(t *testing.T, rate float64) {
	t.Helper()
	prev := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = rate
	t.Cleanup(func() { operation_setting.USDExchangeRate = prev })
}

// 回填历史价时，客户端传来的结算汇率必须被原样冻结。
//
// 这是整个补录功能存在的理由：六月那一版该按六月的汇率结算，而服务端只知道今天的
// 汇率。若这里用服务端汇率覆盖传入值，"冻结汇率、历史成本不漂移"这条不变量恰好在
// 它最该生效的地方被反过来用，且版本行不可变，事后无从修正。
func TestCreateChannelCostVersion_DiscountFreezesClientRate(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 7.3) // 今天的汇率，不该出现在结果里
	const chID = 5101
	seedCostVersionChannel(t, chID)

	c, rec := costVersionPostContext(chID,
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8,"exchange_rate":6.4,"note":"june"}`)
	CreateChannelCostVersion(c)

	resp := decodeCostVersionResponse(t, rec)
	if !resp.Success {
		t.Fatalf("create failed: %s", resp.Message)
	}
	if resp.Data.ExchangeRate != 6.4 {
		t.Fatalf("response exchange_rate = %v, want 6.4 (client value must win)", resp.Data.ExchangeRate)
	}

	versions, err := model.GetChannelCostVersions(chID)
	if err != nil {
		t.Fatalf("load versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(versions))
	}
	if versions[0].ExchangeRate != 6.4 {
		t.Fatalf("stored exchange_rate = %v, want 6.4", versions[0].ExchangeRate)
	}
	// 冻结的汇率必须真的进入定价：0.8 × 6.4 = 5.12。
	ratio, ok := versions[0].EffectiveRatio()
	if !ok || !nearly(ratio, 5.12) {
		t.Fatalf("EffectiveRatio() = %v (ok=%v), want 5.12", ratio, ok)
	}
}

// 不传 exchange_rate 的语义是「按当下的价记一版」，此时服务端汇率就是正确答案。
func TestCreateChannelCostVersion_DiscountFallsBackToServerRate(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 7.3)
	const chID = 5102
	seedCostVersionChannel(t, chID)

	c, rec := costVersionPostContext(chID,
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8}`)
	CreateChannelCostVersion(c)

	resp := decodeCostVersionResponse(t, rec)
	if !resp.Success {
		t.Fatalf("create failed: %s", resp.Message)
	}
	if resp.Data.ExchangeRate != 7.3 {
		t.Fatalf("exchange_rate = %v, want 7.3 (server rate is the default)", resp.Data.ExchangeRate)
	}
}

// 两边都拿不到正数汇率时拒绝写入：把 0 冻进不可变的版本行，该版本覆盖的日志会
// 永久算不出成本（EffectiveRatio 返回 ok=false），只能靠再追加一版绕过。
func TestCreateChannelCostVersion_DiscountRefusesNonPositiveRate(t *testing.T) {
	costIntegrationDB(t)
	withUSDExchangeRate(t, 0)
	const chID = 5103
	seedCostVersionChannel(t, chID)

	for _, body := range []string{
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8}`,
		`{"effective_from":1717200000,"cost_mode":"discount","cost_discount":0.8,"exchange_rate":-1}`,
	} {
		c, rec := costVersionPostContext(chID, body)
		CreateChannelCostVersion(c)

		resp := decodeCostVersionResponse(t, rec)
		if resp.Success {
			t.Fatalf("create succeeded for %s, want refusal", body)
		}
		if !strings.Contains(resp.Message, "positive exchange rate") {
			t.Fatalf("message = %q, want it to name the missing exchange rate", resp.Message)
		}
	}

	versions, err := model.GetChannelCostVersions(chID)
	if err != nil {
		t.Fatalf("load versions: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("versions = %d, want 0 (nothing may be written on refusal)", len(versions))
	}
}
