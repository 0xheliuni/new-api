# 已完成的溯源核对（真实跑通，非示例）

接口：`POST https://openai.com/verify/api/provenance_checks`（网页版后端，Turnstile 现签现用）
脚本：`verify-web-upload.mjs`（CDP 驱动本机 Chrome，零依赖）

## 拿到的结果

| 文件 | C2PA | validation_state | SynthID |
| --- | --- | --- | --- |
| az-out\S6-sunburst-jpeg-c60-0.png | not_detected | not_present | **detected** |
| az-out\M1-mask-edit-0.png | not_detected | not_present | **detected** |
| az-out\R1-gptimage2-1024-0.png | not_detected | not_present | **detected** |
| az-out\R2-gptimage2-1536x1024-0.png | not_detected | not_present | **detected** |
| az-out\E1-sunburst-edit-0.png | **detected** | **trusted** | **detected** |
| az-out\E2-flare-edit-0.png | **detected** | **trusted** | **detected** |
| az-out\E3-gptimage2-edit-0.png | **detected** | **trusted** | **detected** |

本地预检（`provenance-check.mjs --dry-run`，chunk 级，与上表完全吻合）：

- 带 `caBX` chunk → 服务端给 `trusted`：E1/E2/E3、R3、S1、S4/S5/S7/S8/S9/S11、S6 之外的多数 base 图。
- 无 `caBX` → `not_present`：M1、R1、R2、mask-1024.png。
- `S6-sunburst-jpeg-c60-0.png`（JPEG q60 重压缩）：`jumb`/APP11 被丢掉，C2PA 归零，**但 SynthID 仍然 detected**。

## 两个关键结论

1. **C2PA 与 SynthID 是两层独立的信号**。R1/R2/M1 这类"PNG 里没有 caBX"的样本，C2PA 判 not_present，但像素级水印照旧命中 —— 只看 C2PA 会漏判。
2. **JPEG 重编码打不掉 SynthID**（S6 是 92 KB 的 q60 图，依旧 detected）。截图、裁剪、转码这类常规处理对 SynthID 无效，对 C2PA 有效。

## 配额实测

连发上传会返回：

```json
{"error":{"code":"rate_limit_exceeded","message":"You've reached the upload limit for file ..."}}
```

- 间隔 8 s：约第 5 张触发。
- 间隔 25 s：约第 3 张触发（同一时间窗内累计计数未清）。
- 退避 52 s / 102 s / 193 s 仍 429；停 1 分钟后单发立刻恢复。
- 结论：网页接口是**滚动窗口的低配额**，适合零星几张；几十上百张必须走官方 API。

## 待跑完的

剩余 ~27 张（`modex-c2pa-out` 全量 + az-out 剩余）在配额恢复后用：

```bash
node verify-web-upload.mjs az-out modex-c2pa-out -r --delay 60000 --backoff 60000 --max-attempts 8
```

（脚本支持断点续跑：已成功的条目从旧报告里复用，不会重复上传。）
