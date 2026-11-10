package service

import (
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// svgAuthorCharge is what the model that authored an SVG output costs on top of
// the image model. OpenAI bills those tokens at the author model's own rates.
type svgAuthorCharge struct {
	Model string
	// PricedAs names the model whose price was applied, and is empty when no
	// price was available and the tokens went unbilled.
	PricedAs        string
	InputTokens     int
	OutputTokens    int
	ReasoningTokens int
	Quota           decimal.Decimal
}

// calculateSvgAuthorCharge prices the upstream's svg_usage at the group ratio of
// the image request. It returns nil when the response reported no author tokens.
func calculateSvgAuthorCharge(relayInfo *relaycommon.RelayInfo) *svgAuthorCharge {
	usage := relayInfo.SvgUsage
	if usage == nil {
		return nil
	}
	input := max(usage.InputTokens, 0)
	// OpenAI does not say whether output_tokens includes reasoning_tokens. If it
	// does, total minus input equals output; if not, total minus input is larger.
	output := max(usage.OutputTokens, max(usage.TotalTokens, 0)-input, 0)
	if input == 0 && output == 0 {
		return nil
	}

	requested := ""
	if request, ok := relayInfo.Request.(*dto.ImageRequest); ok {
		requested = request.SvgAuthorModel()
	}
	reported := strings.TrimSpace(usage.Model)
	charge := &svgAuthorCharge{
		Model:           common.GetStringIfEmpty(reported, requested),
		InputTokens:     input,
		OutputTokens:    output,
		ReasoningTokens: max(usage.ReasoningTokens, 0),
	}
	groupRatio := decimal.NewFromFloat(relayInfo.PriceData.GroupRatioInfo.GroupRatio)
	for _, name := range []string{reported, requested} {
		if quota, ok := svgAuthorQuotaBeforeGroup(name, input, output); ok {
			charge.PricedAs = name
			charge.Quota = quota.Mul(groupRatio)
			break
		}
	}
	// Mirror a direct call to an unpriced model: a self-use site or a user who
	// accepted unpriced models pays the fallback ratio.
	if charge.PricedAs == "" && charge.Model != "" && (operation_setting.SelfUseModeEnabled || relayInfo.UserSetting.AcceptUnsetRatioModel) {
		fallbackRatio, _, _ := ratio_setting.GetModelRatio(charge.Model)
		charge.PricedAs = charge.Model
		charge.Quota = svgAuthorRatioQuota(charge.Model, fallbackRatio, input, output).Mul(groupRatio)
	}
	// A negative group ratio must not turn the author's tokens into a credit.
	if charge.Quota.IsNegative() {
		charge.Quota = decimal.Zero
	}
	return charge
}

// svgAuthorQuotaBeforeGroup prices the tokens the way a direct call to the model
// is priced: expression, then per-call price, then ratio. The second result is
// false when the name has no usable price; a negative price is not usable.
func svgAuthorQuotaBeforeGroup(name string, input, output int) (decimal.Decimal, bool) {
	if name == "" {
		return decimal.Zero, false
	}
	if billing_setting.GetBillingMode(name) == billing_setting.BillingModeTieredExpr {
		if expression, ok := billing_setting.GetBillingExpr(name); ok {
			// The author call is not a client request, so request-dependent rules
			// evaluate against an empty context.
			result, err := billingexpr.ComputeTieredQuota(&billingexpr.BillingSnapshot{
				ExprString:   expression,
				ExprHash:     billingexpr.ExprHashString(expression),
				GroupRatio:   1,
				QuotaPerUnit: common.QuotaPerUnit,
				ExprVersion:  billingexpr.ExprVersion(expression),
			}, billingexpr.TokenParams{P: float64(input), C: float64(output), Len: float64(input)})
			quota := result.ActualQuotaBeforeGroup
			if err != nil || math.IsNaN(quota) || math.IsInf(quota, 0) || quota < 0 {
				return decimal.Zero, false
			}
			return decimal.NewFromFloat(quota), true
		}
	}
	var quota decimal.Decimal
	if price, ok := ratio_setting.GetModelPrice(name, false); ok {
		quota = decimal.NewFromFloat(price).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	} else if ratio_setting.HasConfiguredModelRatio(name) {
		modelRatio, _, _ := ratio_setting.GetModelRatio(name)
		quota = svgAuthorRatioQuota(name, modelRatio, input, output)
	} else {
		return decimal.Zero, false
	}
	return quota, !quota.IsNegative()
}

func svgAuthorRatioQuota(name string, modelRatio float64, input, output int) decimal.Decimal {
	return decimal.NewFromInt(int64(input)).
		Add(decimal.NewFromInt(int64(output)).Mul(decimal.NewFromFloat(ratio_setting.GetCompletionRatio(name)))).
		Mul(decimal.NewFromFloat(modelRatio))
}

// appendSvgAuthorLogInfo records the author's tokens and cost on the consume
// log. An author without a price is also reported to administrators, because
// its tokens went unbilled.
func appendSvgAuthorLogInfo(ctx *gin.Context, other *model.LogOther, author *svgAuthorCharge) {
	if author == nil {
		return
	}
	other.SetPublic("svg_author", map[string]any{
		"model":            author.Model,
		"priced_as":        author.PricedAs,
		"input_tokens":     author.InputTokens,
		"output_tokens":    author.OutputTokens,
		"reasoning_tokens": author.ReasoningTokens,
		"quota":            common.QuotaFromDecimal(author.Quota),
	})
	if author.PricedAs == "" {
		other.SetAdmin("svg_author_unpriced", author.Model)
		logger.LogWarn(ctx, common.LogText("SVG author model %s could not be priced; its tokens were not charged", author.Model))
	}
}
