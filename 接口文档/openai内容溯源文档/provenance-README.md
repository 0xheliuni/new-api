# OpenAI 图片溯源（C2PA + SynthID）接口说明

网页版：<https://openai.com/research/verify/>（`/zh-Hans-CN/` 只是语言前缀，接口同一个）

## 一、网页端的"限制"到底是什么

前端没有任何加密校验，真正的门槛只有三样：

| 限制 | 实质 | 绕法 |
| --- | --- | --- |
| 一次只能拖一张、只能手点 | 纯 UI 限制，后端是普通 multipart 接口 | 直接对接口发 POST，循环即可 |
| Cloudflare Turnstile | 请求头 `X-Turnstile-Token` 必须带**新签的、一次性的** token | 每次上传前 `window.turnstile.reset(0)` + `execute(0)` 现签一个 |
| Cloudflare 会话/指纹 | 接口挂在 openai.com 域下，需要 cf cookie + 一致的 UA/指纹 | 把 upload 放进页面上下文里 `fetch`（cookie/Origin/Referer 天然正确） |

实测确认的真实接口（2026-10 校验通过）：

```
POST https://openai.com/verify/api/provenance_checks
Content-Type: multipart/form-data
Headers:
  Origin:  https://openai.com
  Referer: https://openai.com/research/verify/
  X-Turnstile-Token: <cf-turnstile-response 的值>
Body:
  file=<二进制>          # 字段名就是 file，单文件
```

返回体（与官方 API 完全同构）：

```json
{
  "object": "provenance_check",
  "created_at": 1779225920,
  "results": [
    { "type": "c2pa",    "outcome": "detected",     "validation_state": "trusted", "issuer": "OpenAI OpCo, LLC" },
    { "type": "synthid", "outcome": "not_detected" }
  ]
}
```

## 二、两条可用路径

### A. 官方 API（干净、可批量、无 Turnstile）

```
POST https://api.openai.com/v1/content_provenance_checks
Authorization: Bearer $OPENAI_API_KEY
multipart: file=@image.png;type=image/png
```

- 同步返回，不用轮询、不用 Files API。
- 支持 PNG / JPEG / WebP，单文件 ≤ 50 MiB；音频 MP3/Opus/AAC/FLAC/WAV/PCM，≤ 60 s。
- 组织没开通该接口会返回 **404**（需申请访问）；400 = 格式/内容被拒；429 带 `Retry-After`。
- 脚本：`node provenance-check.mjs --dir <目录> -r`

### B. 网页接口直连（没有 API 权限时用）

用本机 Chrome 通过 CDP 打开页面签 Turnstile，再在页面上下文里 fetch 上传：

```bash
node verify-web-upload.mjs --dir az-out              # 批量
node verify-web-upload.mjs -r modex-c2pa-out --limit 5
node verify-web-upload.mjs --discover                # 只抓真实请求，校核 endpoint 是否改名
node verify-web-upload.mjs xx.png --node-post        # 改由 Node 侧发 multipart（调试用）
```

要点：

1. `window.turnstile` 是懒加载的，页面 load 后要轮询等它就绪（脚本已做，最多 90 s）。
2. token **一次一签**，复用会 403。脚本每个文件都重新签。
3. 上传放在 `Runtime.evaluate` 里的 `fetch` 执行 —— Origin/Referer/cf cookie/TLS 指纹全部与真人一致，比在 Node 里拼 header 稳。
4. 别用 `--headless`：Turnstile 在无头 Chrome 上大概率要交互挑战。
5. 批量之间留 400–800 ms 间隔，否则会撞 CF 限流。

## 三、本地能做到哪一步

| 信号 | 能否本地判 | 说明 |
| --- | --- | --- |
| C2PA 清单存在 | ✅ | PNG 找 `caBX` chunk、JPEG 找 `APP11/jumb`、WebP 找 `C2PA` chunk |
| C2PA 硬绑定校验 | ✅ | 复算 `c2pa.hash.data` 的 SHA-256（按 exclusions 剔除后）与清单比对，一致即证明清单绑死在**这份**像素上 |
| C2PA 信任链（`trusted` / `valid` / `invalid`） | ⚠️ 需证书链 | 用 `c2patool` / `c2pa-python` 走官方 trust list 才能给出 `validation_state` |
| SynthID 像素水印 | ❌ | 检测器不在客户端，只有 OpenAI 服务端能判。所以 JPEG 重压缩后 C2PA 没了、SynthID 还在的场景，必须走 A 或 B |

`provenance-check.mjs --dry-run` 就是这个本地预检（chunk 级），能先筛掉明显没有清单的文件，省得白跑网络请求。

## 四、实测记录（本机，2026-10-29）

- 网页接口确认可用，返回体与官方 API 同构；`POST https://openai.com/verify/api/provenance_checks` 出现在页面网络请求里。
- Turnstile 在**非无头** Chrome 上自动签 token，无需人工点复选框；`window.turnstile` 是懒加载，load 后约 1–2 s 才出现（脚本轮询等它）。
- 突发上传会被挡：

  ```json
  {"error":{"code":"rate_limit_exceeded","message":"You've reached the upload limit for file ..."}}
  ```

  约 4–5 张连发即触发；**窗口很短**（几分钟），期间指数退避即可恢复，但退避的每次重试都必须换新 token。
  建议 `--delay 25000`（25 s/张）；要跑几十张就别省时间用 8 s，那样只会连续 429 把窗口续命。
- 想真正批量（几百张）→ 只能走官方 API，别在网页接口上耗。

## 五、结果怎么读

- C2PA 的 `outcome=detected` 只在 **issuer 是 OpenAI 且含 AI 生成动作**、且 `validation_state` 为 `trusted`/`valid` 时才成立；第三方清单、无生成动作、`invalid`、`not_present` 统统记 `not_detected`。
- `not_detected` ≠ 不是 AI 生成：元数据被剥、截图、裁剪、转码、老模型都会导致它。SynthID 才是抗转码的那一层。
- 反过来 `detected` 也只说明"存在这个信号"，不是完整创作史。
