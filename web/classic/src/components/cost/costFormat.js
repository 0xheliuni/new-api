/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// 成本报表的金额口径：后端每个金额字段的货币由字段名声明 —— revenue_usd /
// list_usd / refund_usd 是美元，cost_cny / profit_cny 是人民币（成本 = 刊例$ ×
// 渠道倍率，倍率单位是「每 $1 刊例记 ¥N」）。展示时按查询汇率换算，主副行是
// 同一笔钱的两种货币：
//
//   原生 USD → $N，¥ (N × 汇率)
//   原生 CNY → ¥N，$ (N ÷ 汇率)
//
// 额度展示类型 (quota_display_type，与 helpers/render.jsx 同一个 localStorage
// key) 只决定哪种货币做主行。把 cost_cny 直接贴 $ 会让成本虚高汇率倍。
// 图表只有一条轴，走 getCostChartCurrency().fromCny() 换算一次后贴符号。

import { getCurrencyConfig } from '../../helpers/render';

/** 读取管理员配置的额度展示类型，兜底 'USD'。 */
export function getQuotaDisplayType() {
  return localStorage.getItem('quota_display_type') || 'USD';
}

/** 成本报表双币种展示时，哪种货币显示在主行：仅当管理员显式选择 CNY 时才是 CNY 主行。 */
export function getMoneyPrimaryCurrency() {
  return getQuotaDisplayType() === 'CNY' ? 'cny' : 'usd';
}

export function formatUsd(value, digits = 2) {
  return `$${Number(value || 0).toFixed(digits)}`;
}

export function formatCny(value, digits = 2) {
  return `¥${Number(value || 0).toFixed(digits)}`;
}

/**
 * 成本报表的双币种金额：按字段原生货币换算出 USD/CNY 两个值，展示类型决定主行。
 *
 * @param {number} amount 后端返回的金额
 * @param {'usd'|'cny'} native 该字段的原生货币（*_usd / *_cny）
 * @param {'usd'|'cny'} primary 展示类型决定的主行货币
 * @param {number} exchangeRate 查询汇率
 * @param {number} digits
 * @returns {{primary: string, secondary: string}}
 */
export function resolveDualMoney(
  amount,
  native,
  primary,
  exchangeRate,
  digits = 2,
) {
  const n = Number(amount || 0);
  const usd = native === 'usd' ? n : deriveUsdFromCny(n, exchangeRate);
  const cny = native === 'cny' ? n : deriveCnyFromUsd(n, exchangeRate);
  return primary === 'cny'
    ? { primary: formatCny(cny, digits), secondary: formatUsd(usd, digits) }
    : { primary: formatUsd(usd, digits), secondary: formatCny(cny, digits) };
}

/** 用当前查询生效的汇率，把仅有人民币的字段（成本/利润）换算出美元。 */
export function deriveUsdFromCny(cny, exchangeRate) {
  return Number(exchangeRate) > 0 ? Number(cny || 0) / Number(exchangeRate) : 0;
}

/** 用当前查询生效的汇率，把仅有美元的字段（刊例价）换算出人民币。 */
export function deriveCnyFromUsd(usd, exchangeRate) {
  return Number(usd || 0) * Number(exchangeRate || 0);
}

/** 倍率展示：×1.4 这种形式，最多 4 位小数且去掉尾随 0。 */
export function formatCostRatio(ratio) {
  const n = Number(ratio || 0);
  if (!n) return '-';
  return `×${Number(n.toFixed(4))}`;
}

/**
 * 用户行的实际生效成本倍率：用户跨多个渠道，没有单一配置倍率，
 * 只能用这一行自己的钱反推 —— cost_cny / (list_usd × 汇率)。
 * 与后端 cost_cny = list_usd × ratio 互为逆运算，因此结果就是加权倍率。
 * 刊例价为 0（无上游计费基数）时返回 null，由调用方渲染 '-'。
 */
export function effectiveCostRatioOf(row) {
  const listUsd = Number(row?.list_usd || 0);
  const costCny = Number(row?.cost_cny || 0);
  if (!listUsd) return null;
  return costCny / listUsd;
}

/**
 * 图表的单一展示货币，跟随「额度展示类型」。
 *
 * 表格/KPI 能并排放两种货币，图表只有一条轴，必须收敛成一种：
 * CNY → ¥；CUSTOM → 自定义符号；
 * USD / TOKENS → $（金额用 token 表达无意义，与新前端 getBillingDisplayMeta 同口径）。
 *
 * 趋势/堆叠点只带 *_cny（按查询汇率算出）。fromCny() 把它换算一次成展示货币：
 * 展示 CNY 原样；其余先按查询汇率折回美元，再乘站点 USD→展示货币汇率。
 * format() 只贴符号。
 *
 * @returns {{symbol: string, rateFromUsd: number, fromCny: (cny: number, queryRate: number) => number, format: (amount: number) => string}}
 */
export function getCostChartCurrency() {
  const { symbol, rate, type } = getCurrencyConfig();
  // TOKENS 没有货币语义，退回美元；其余沿用 getCurrencyConfig 的符号与汇率。
  const isTokens = type === 'TOKENS';
  const displaySymbol = isTokens ? '$' : symbol;
  const rateFromUsd = isTokens ? 1 : Number(rate) || 1;
  return {
    symbol: displaySymbol,
    rateFromUsd,
    fromCny: (cny, queryRate) =>
      type === 'CNY'
        ? Number(cny) || 0
        : deriveUsdFromCny(cny, queryRate) * rateFromUsd,
    format: (amount, digits = 2) =>
      `${displaySymbol}${Number(amount || 0).toFixed(digits)}`,
  };
}
