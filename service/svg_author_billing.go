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
	// InputPriceUSD and OutputPriceUSD are the author model's list prices per
	// million tokens, before the group ratio. They stay nil when the model is
	// priced per request, which has no per-token rate.
	InputPriceUSD  *float64
	OutputPriceUSD *float64
	Quota          decimal.Decimal
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
		if quota, inputPrice, outputPrice, ok := svgAuthorQuotaBeforeGroup(name, input, output); ok {
			charge.PricedAs = name
			charge.InputPriceUSD = inputPrice
			charge.OutputPriceUSD = outputPrice
			charge.Quota = quota.Mul(groupRatio)
			break
		}
	}
	// Mirror a direct call to an unpriced model: a self-use site or a user who
	// accepted unpriced models pays the fallback ratio.
	if charge.PricedAs == "" && charge.Model != "" && (operation_setting.SelfUseModeEnabled || relayInfo.UserSetting.AcceptUnsetRatioModel) {
		fallbackRatio, _, _ := ratio_setting.GetModelRatio(charge.Model)
		charge.PricedAs = charge.Model
		charge.InputPriceUSD = ratioPricePerMillion(fallbackRatio)
		if charge.InputPriceUSD != nil {
			outputPrice, _ := decimal.NewFromFloat(*charge.InputPriceUSD).
				Mul(decimal.NewFromFloat(ratio_setting.GetCompletionRatio(charge.Model))).Round(6).Float64()
			charge.OutputPriceUSD = &outputPrice
		}
		charge.Quota = svgAuthorRatioQuota(charge.Model, fallbackRatio, input, output).Mul(groupRatio)
	}
	// A negative group ratio must not turn the author's tokens into a credit.
	if charge.Quota.IsNegative() {
		charge.Quota = decimal.Zero
	}
	return charge
}

// svgAuthorQuotaBeforeGroup prices the tokens the way a direct call to the model
// is priced: expression, then per-call price, then ratio. The remaining results
// are the model's list prices per million tokens before the group ratio, which
// stay nil for per-request pricing. ok is false when the name has no usable
// price; a negative price is not usable.
func svgAuthorQuotaBeforeGroup(name string, input, output int) (quota decimal.Decimal, inputPrice, outputPrice *float64, ok bool) {
	if name == "" {
		return decimal.Zero, nil, nil, false
	}
	if billing_setting.GetBillingMode(name) == billing_setting.BillingModeTieredExpr {
		if expression, ok := billing_setting.GetBillingExpr(name); ok {
			// The author call is not a client request, so request-dependent rules
			// evaluate against an empty context.
			snapshot := &billingexpr.BillingSnapshot{
				ExprString:   expression,
				ExprHash:     billingexpr.ExprHashString(expression),
				GroupRatio:   1,
				QuotaPerUnit: common.QuotaPerUnit,
				ExprVersion:  billingexpr.ExprVersion(expression),
			}
			total, err := billingexpr.ComputeTieredQuota(snapshot, billingexpr.TokenParams{P: float64(input), C: float64(output), Len: float64(input)})
			if err != nil || !isFiniteNonNegative(total.ActualQuotaBeforeGroup) {
				return decimal.Zero, nil, nil, false
			}
			// A request-priced author has no per-token rate to show.
			if total.BillingUnit == billingexpr.BillingUnitRequest {
				return decimal.NewFromFloat(total.ActualQuotaBeforeGroup), nil, nil, true
			}
			// The expression has no per-token coefficients to read, so its input
			// and output prices are recovered by running it twice: with the
			// output tokens removed, and with no tokens at all (constant terms).
			inputOnly, err := billingexpr.ComputeTieredQuota(snapshot, billingexpr.TokenParams{P: float64(input), Len: float64(input)})
			if err != nil || !isFiniteNonNegative(inputOnly.ActualQuotaBeforeGroup) {
				return decimal.Zero, nil, nil, false
			}
			constantOnly, err := billingexpr.ComputeTieredQuota(snapshot, billingexpr.TokenParams{})
			if err != nil || !isFiniteNonNegative(constantOnly.ActualQuotaBeforeGroup) {
				return decimal.Zero, nil, nil, false
			}
			return decimal.NewFromFloat(total.ActualQuotaBeforeGroup),
				quotaPerMillion(inputOnly.ActualQuotaBeforeGroup-constantOnly.ActualQuotaBeforeGroup, input),
				quotaPerMillion(total.ActualQuotaBeforeGroup-inputOnly.ActualQuotaBeforeGroup, output),
				true
		}
	}
	if price, ok := ratio_setting.GetModelPrice(name, false); ok {
		quota = decimal.NewFromFloat(price).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	} else if ratio_setting.HasConfiguredModelRatio(name) {
		modelRatio, _, _ := ratio_setting.GetModelRatio(name)
		quota = svgAuthorRatioQuota(name, modelRatio, input, output)
		inputPrice = ratioPricePerMillion(modelRatio)
		if inputPrice != nil {
			completionRatio := ratio_setting.GetCompletionRatio(name)
			price, _ := decimal.NewFromFloat(*inputPrice).
				Mul(decimal.NewFromFloat(completionRatio)).Round(6).Float64()
			outputPrice = &price
		}
	} else {
		return decimal.Zero, nil, nil, false
	}
	return quota, inputPrice, outputPrice, !quota.IsNegative()
}

func isFiniteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

// quotaPerMillion converts a quota amount for tokens into a USD price per
// million tokens. It returns nil when tokens is not positive.
func quotaPerMillion(quota float64, tokens int) *float64 {
	if tokens <= 0 {
		return nil
	}
	price, _ := decimal.NewFromFloat(quota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromInt(1_000_000)).
		Div(decimal.NewFromInt(int64(tokens))).
		Round(6).Float64()
	return &price
}

// ratioPricePerMillion converts a legacy model ratio into its USD price per
// million input tokens: one ratio unit is two dollars per million.
func ratioPricePerMillion(modelRatio float64) *float64 {
	if modelRatio <= 0 {
		return nil
	}
	price, _ := decimal.NewFromFloat(modelRatio).
		Div(decimal.NewFromFloat(common.QuotaPerUnit / 1_000_000)).
		Round(6).Float64()
	return &price
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
	// List prices per million tokens, written only when the model is priced per
	// token so the log can show how the author's cost was derived.
	if author.InputPriceUSD != nil {
		other.SetPublic("svg_author_input_price", *author.InputPriceUSD)
	}
	if author.OutputPriceUSD != nil {
		other.SetPublic("svg_author_output_price", *author.OutputPriceUSD)
	}
	if author.PricedAs == "" {
		other.SetAdmin("svg_author_unpriced", author.Model)
		logger.LogWarn(ctx, common.LogText("SVG author model %s could not be priced; its tokens were not charged", author.Model))
	}
}
