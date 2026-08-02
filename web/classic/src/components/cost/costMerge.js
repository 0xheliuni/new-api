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

// 展开行客户端合并工具：与后端 controller/cost_stat.go 的 costMoney.add()/
// deriveRates() 口径保持一致 —— 只对"原始可加字段"求和，派生指标（成功率/
// 缓存率/平均TTFT/利润率）在求和后统一重新计算，绝不对派生字段直接求平均。

// 原始可加字段：来自 costMoney（后端 dto），breakdown 行内嵌展开为同级字段。
export const RAW_ADDITIVE_FIELDS = [
  'revenue_usd',
  'revenue_cny',
  'list_usd',
  'cost_cny',
  'profit_cny',
  'refund_usd',
  'prompt_tokens',
  'completion_tokens',
  'request_count',
  'cache_read_tokens',
  'cache_creation_tokens',
  'error_count',
  'frt_sum_ms',
  'frt_count',
];

// 重新派生 v2 指标，零分母兜底规则与后端 costMoney.deriveRates() 一致：
// - success_rate：request_count+error_count 为 0 时兜底为 1
// - cache_rate：prompt_tokens 为 0 时兜底为 0
// - avg_ttft_ms：frt_count 为 0 时兜底为 0
// - profit_rate：revenue_cny 为 0 时兜底为 0
export function deriveCostRates(row) {
  const promptTokens = Number(row.prompt_tokens) || 0;
  const completionTokens = Number(row.completion_tokens) || 0;
  const requestCount = Number(row.request_count) || 0;
  const errorCount = Number(row.error_count) || 0;
  const cacheReadTokens = Number(row.cache_read_tokens) || 0;
  const frtSumMs = Number(row.frt_sum_ms) || 0;
  const frtCount = Number(row.frt_count) || 0;
  const revenueCny = Number(row.revenue_cny) || 0;
  const profitCny = Number(row.profit_cny) || 0;

  row.total_tokens = promptTokens + completionTokens;
  row.success_rate =
    requestCount + errorCount === 0 ? 1 : requestCount / (requestCount + errorCount);
  row.cache_rate = promptTokens === 0 ? 0 : cacheReadTokens / promptTokens;
  row.avg_ttft_ms = frtCount === 0 ? 0 : frtSumMs / frtCount;
  row.profit_rate = revenueCny === 0 ? 0 : profitCny / revenueCny;
  return row;
}

/**
 * 按 keyFields 合并 breakdown 明细行：对 RAW_ADDITIVE_FIELDS 求和，重新派生比率指标。
 * keyFields 为空（null/undefined/[]）时视为"明细"模式，原样返回（不合并）。
 *
 * @param {Array<object>} rows 展开行明细（costBreakdownRow[]，字段已在 JSON 中展平）
 * @param {string[]|null} keyFields 保留分组的身份字段，如 ['channel_id','channel_name']
 * @returns {Array<object>} 合并后的行（新对象，不修改入参）
 */
export function mergeBreakdown(rows, keyFields) {
  if (!Array.isArray(rows) || rows.length === 0) return [];
  if (!keyFields || keyFields.length === 0) return rows;

  const groups = new Map();
  const order = [];
  for (const row of rows) {
    const key = keyFields.map((f) => row[f] ?? '').join('␟');
    let g = groups.get(key);
    if (!g) {
      g = {};
      keyFields.forEach((f) => {
        g[f] = row[f];
      });
      RAW_ADDITIVE_FIELDS.forEach((f) => {
        g[f] = 0;
      });
      groups.set(key, g);
      order.push(key);
    }
    RAW_ADDITIVE_FIELDS.forEach((f) => {
      g[f] = (Number(g[f]) || 0) + (Number(row[f]) || 0);
    });
  }
  return order.map((key) => deriveCostRates(groups.get(key)));
}

// 每个维度 Tab 下「查看方式」Select 的可选项：value 唯一标识，keyFields 传给
// mergeBreakdown（null = 明细，不合并）。中文 label 走 i18n key 原样翻译。
export const MERGE_VIEW_OPTIONS = {
  users: [
    { value: 'detail', labelKey: '明细', keyFields: null },
    {
      value: 'merge_model',
      labelKey: '合并模型(按渠道汇总)',
      keyFields: ['channel_id', 'channel_name'],
    },
    {
      value: 'merge_channel',
      labelKey: '合并渠道(按模型汇总)',
      keyFields: ['model_name'],
    },
  ],
  models: [
    { value: 'detail', labelKey: '明细', keyFields: null },
    {
      value: 'merge_user',
      labelKey: '合并用户',
      keyFields: ['channel_id', 'channel_name'],
    },
    { value: 'merge_channel', labelKey: '合并渠道', keyFields: ['username'] },
  ],
  channels: [
    { value: 'detail', labelKey: '明细', keyFields: null },
    { value: 'merge_user', labelKey: '合并用户', keyFields: ['model_name'] },
    { value: 'merge_model', labelKey: '合并模型', keyFields: ['username'] },
  ],
};
