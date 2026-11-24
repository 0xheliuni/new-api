#!/usr/bin/env node
/**
 * provenance-check.mjs — OpenAI 内容溯源（C2PA + SynthID）批量检查器
 *
 * 走官方接口：POST https://api.openai.com/v1/content_provenance_checks
 * 没有 Turnstile、没有前端限制，可一次跑完整个目录。
 *
 * 用法：
 *   set OPENAI_API_KEY=sk-...                       # 或 --key / --key-file
 *   node provenance-check.mjs                        # 默认扫 ./ 下 png/jpeg/webp
 *   node provenance-check.mjs --dir az-out --dry-run # 只做本地预检，不上传
 *   node provenance-check.mjs az-out/S6-sunburst-jpeg-c60-0.png modex-c2pa-out/gpt-image-2-0.png
 *   node provenance-check.mjs --dir . --concurrency 3 --out report.json --recurse
 *
 * 退出码：0 全部成功；1 有失败项（详情见报告）。
 */

import fs from 'node:fs';
import fsp from 'node:fs/promises';
import path from 'node:path';

const DEFAULT_BASE = 'https://api.openai.com/v1';
const EXT_MIME = {
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.mp3': 'audio/mpeg',
  '.opus': 'audio/ogg',
  '.ogg': 'audio/ogg',
  '.aac': 'audio/aac',
  '.flac': 'audio/flac',
  '.wav': 'audio/wav',
  '.pcm': 'audio/pcm',
};
const MAX_BYTES = 50 * 1024 * 1024;

// ---------------------------------------------------------------- args
function parseArgs(argv) {
  const o = {
    targets: [],
    dir: null,
    recurse: false,
    key: process.env.OPENAI_API_KEY || '',
    keyFile: null,
    baseUrl: process.env.OPENAI_BASE_URL || DEFAULT_BASE,
    concurrency: 2,
    out: 'provenance-report.json',
    dryRun: false,
    limit: 0,
    timeoutMs: 120000,
  };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const next = () => argv[++i];
    if (a === '--dir') o.dir = next();
    else if (a === '--recurse' || a === '-r') o.recurse = true;
    else if (a === '--key') o.key = next();
    else if (a === '--key-file') o.keyFile = next();
    else if (a === '--base-url') o.baseUrl = next().replace(/\/+$/, '');
    else if (a === '--concurrency' || a === '-c') o.concurrency = Math.max(1, Number(next()) || 1);
    else if (a === '--out') o.out = next();
    else if (a === '--limit') o.limit = Number(next()) || 0;
    else if (a === '--timeout') o.timeoutMs = Math.max(1000, Number(next()) || 120000);
    else if (a === '--dry-run') o.dryRun = true;
    else if (a === '--help' || a === '-h') o.help = true;
    else if (!a.startsWith('--')) o.targets.push(a);
  }
  if (o.keyFile) {
    try {
      o.key = fs.readFileSync(o.keyFile, 'utf8').trim();
    } catch (e) {
      o.key = '';
      o.keyFileError = e.message;
    }
  }
  return o;
}

function usage() {
  console.log(`provenance-check.mjs — OpenAI 内容溯源批量检查

  node provenance-check.mjs [文件或目录...] [选项]

  --dir DIR          目录（等同传目录参数）
  -r, --recurse      递归子目录
  --key KEY          API Key（默认读环境变量 OPENAI_API_KEY）
  --key-file PATH    从文件读 Key
  --base-url URL     默认 ${DEFAULT_BASE}
  -c, --concurrency  并发，默认 2
  --out PATH         报告路径，默认 provenance-report.json
  --limit N          只处理前 N 个文件
  --timeout MS       单请求超时，默认 120000
  --dry-run          只在本地解析格式/大小，不上传

结果字段：c2pa.outcome / c2pa.validation_state / c2pa.issuer / c2pa.model /
c2pa.generated_at / synthid.outcome`);
}

// ---------------------------------------------------------------- collect
async function collect(targets, { recurse, dir }) {
  const roots = [...targets];
  if (dir) roots.push(dir);
  if (!roots.length) roots.push('.');
  const files = new Set();
  const walk = async (p) => {
    const st = await fsp.stat(p).catch(() => null);
    if (!st) return;
    if (st.isDirectory()) {
      const entries = await fsp.readdir(p, { withFileTypes: true });
      for (const e of entries) {
        if (e.name === 'node_modules' || e.name.startsWith('.git')) continue;
        const full = path.join(p, e.name);
        if (e.isDirectory()) {
          if (recurse) await walk(full);
        } else if (EXT_MIME[path.extname(e.name).toLowerCase()]) files.add(full);
      }
    } else if (EXT_MIME[path.extname(p).toLowerCase()]) {
      files.add(p);
    }
  };
  for (const r of roots) await walk(r);
  return [...files].sort();
}

// ---------------------------------------------------------------- local sniff
function sniff(buf, ext) {
  const info = { mime: EXT_MIME[ext] || 'application/octet-stream', magic: 'unknown', c2pa_hint: null };
  if (buf.length > 12 && buf.readUInt32BE(0) === 0x89504e47) {
    info.magic = 'png';
    const chunks = [];
    let off = 8;
    while (off + 8 <= buf.length) {
      const len = buf.readUInt32BE(off);
      const type = buf.toString('latin1', off + 4, off + 8);
      chunks.push(type);
      off += 12 + len;
      if (type === 'IEND' || off < 0) break;
    }
    info.png_chunks = chunks;
    if (chunks.includes('caBX')) info.c2pa_hint = 'caBX chunk present';
    else if (chunks.includes('iTXt')) info.c2pa_hint = 'iTXt only (no caBX)';
  } else if (buf.length > 3 && buf[0] === 0xff && buf[1] === 0xd8 && buf[2] === 0xff) {
    info.magic = 'jpeg';
    info.c2pa_hint = buf.includes(Buffer.from('jumb')) ? 'jumb box present' : 'no jumb (APP11) box';
  } else if (buf.length > 12 && buf.toString('latin1', 0, 4) === 'RIFF' && buf.toString('latin1', 8, 12) === 'WEBP') {
    info.magic = 'webp';
    info.c2pa_hint = buf.includes(Buffer.from('C2PA')) ? 'C2PA chunk present' : 'no C2PA chunk';
  }
  return info;
}

// ---------------------------------------------------------------- upload
async function uploadOne(file, opt) {
  const buf = await fsp.readFile(file);
  const ext = path.extname(file).toLowerCase();
  const local = sniff(buf, ext);
  const row = {
    file: path.relative(process.cwd(), file) || file,
    bytes: buf.length,
    mime: local.mime,
    magic: local.magic,
    c2pa_hint: local.c2pa_hint,
    status: 'skipped',
  };
  if (buf.length === 0) return { ...row, status: 'error', error: 'empty file' };
  if (buf.length > MAX_BYTES) return { ...row, status: 'error', error: 'over 50MiB' };
  if (opt.dryRun) return { ...row, status: 'dry-run' };

  const url = `${opt.baseUrl}/content_provenance_checks`;
  let lastErr = null;
  for (let attempt = 1; attempt <= 4; attempt++) {
    const fd = new FormData();
    fd.append('file', new Blob([buf], { type: local.mime }), path.basename(file));
    try {
      const res = await fetch(url, {
        method: 'POST',
        headers: { Authorization: `Bearer ${opt.key}` },
        body: fd,
        signal: AbortSignal.timeout(opt.timeoutMs),
      });
      const text = await res.text();
      let body = null;
      try {
        body = JSON.parse(text);
      } catch {
        body = { raw: text.slice(0, 800) };
      }
      if (res.status === 200) {
        const results = body.results || [];
        const c2pa = results.find((r) => r.type === 'c2pa') || null;
        const synthid = results.find((r) => r.type === 'synthid') || null;
        return {
          ...row,
          status: 'ok',
          created_at: body.created_at,
          c2pa,
          synthid,
          openai_detected: !!(c2pa?.outcome === 'detected' || synthid?.outcome === 'detected'),
        };
      }
      // 429/5xx 重试
      if ([429, 500, 502, 503, 504].includes(res.status) && attempt < 4) {
        const ra = Number(res.headers.get('retry-after')) || attempt * 2;
        await new Promise((r) => setTimeout(r, Math.min(ra, 30) * 1000));
        lastErr = { code: res.status, body };
        continue;
      }
      const hint =
        res.status === 404
          ? '组织无 Content Provenance API 权限（需申请访问），改用 verify-web-upload.mjs'
          : res.status === 400
            ? '文件格式/大小/内容被拒'
            : res.status === 401
              ? 'API Key 无效'
              : undefined;
      return { ...row, status: 'error', http: res.status, error: body?.error?.message || body?.error?.code || 'http error', hint, body };
    } catch (e) {
      lastErr = { code: 'network', body: e.message };
      if (attempt < 4) {
        await new Promise((r) => setTimeout(r, attempt * 1500));
        continue;
      }
    }
  }
  return { ...row, status: 'error', http: lastErr?.code, error: typeof lastErr?.body === 'string' ? lastErr.body : JSON.stringify(lastErr?.body) };
}

// ---------------------------------------------------------------- main
async function main() {
  const opt = parseArgs(process.argv.slice(2));
  if (opt.help) return usage();

  const files = await collect(opt.targets, opt);
  if (!files.length) {
    console.error('没找到可处理的图片/音频（png/jpg/jpeg/webp/mp3/opus/aac/flac/wav）。');
    process.exit(1);
  }
  if (opt.limit) files.splice(opt.limit);

  if (!opt.dryRun) {
    if (!opt.key) {
      console.error('缺少 API Key：设置 OPENAI_API_KEY，或用 --key / --key-file 指定。');
      console.error('想先验证本地 C2PA 结构可用：node provenance-check.mjs --dry-run');
      process.exit(1);
    }
    console.log(`接口: ${opt.baseUrl}/content_provenance_checks  文件数: ${files.length}  并发: ${opt.concurrency}\n`);
  } else {
    console.log(`dry-run：本地预检 ${files.length} 个文件\n`);
  }

  const rows = new Array(files.length);
  let cursor = 0;
  const worker = async () => {
    while (true) {
      const i = cursor++;
      if (i >= files.length) return;
      rows[i] = await uploadOne(files[i], opt);
      const r = rows[i];
      const tag =
        r.status === 'ok'
          ? `C2PA=${r.c2pa?.outcome || '-'}/${r.c2pa?.validation_state || '-'} SynthID=${r.synthid?.outcome || '-'}`
          : r.status;
      process.stdout.write(`[${String(i + 1).padStart(3)}/${files.length}] ${tag.padEnd(46)} ${r.file}\n`);
    }
  };
  await Promise.all(Array.from({ length: Math.min(opt.concurrency, files.length) }, worker));

  const ok = rows.filter((r) => r.status === 'ok');
  const detected = ok.filter((r) => r.openai_detected);
  const report = {
    generated_at: new Date().toISOString(),
    endpoint: opt.dryRun ? null : `${opt.baseUrl}/content_provenance_checks`,
    summary: {
      total: rows.length,
      ok: ok.length,
      openai_detected: detected.length,
      error: rows.filter((r) => r.status === 'error').length,
      skipped: rows.filter((r) => r.status !== 'ok' && r.status !== 'error').length,
    },
    results: rows,
  };
  await fsp.writeFile(opt.out, JSON.stringify(report, null, 2), 'utf8');

  console.log('\n================ 汇总 ================');
  if (ok.length) {
    const pad = (s, n) => String(s ?? '-').padEnd(n).slice(0, n);
    console.log(`${pad('file', 52)}${pad('c2pa', 13)}${pad('state', 13)}${pad('synthid', 11)}model`);
    console.log('-'.repeat(110));
    for (const r of ok) {
      console.log(
        `${pad(r.file, 52)}${pad(r.c2pa?.outcome, 13)}${pad(r.c2pa?.validation_state, 13)}${pad(r.synthid?.outcome, 11)}${r.c2pa?.model ?? r.synthid?.model ?? '-'}`,
      );
    }
  }
  for (const r of rows.filter((x) => x.status === 'error')) {
    console.log(`ERR  ${r.file}  http=${r.http}  ${r.error}${r.hint ? '  ← ' + r.hint : ''}`);
  }
  console.log(`\n报告: ${path.resolve(opt.out)}`);
  process.exit(rows.some((r) => r.status === 'error') ? 1 : 0);
}

main().catch((e) => {
  console.error('fatal:', e);
  process.exit(1);
});
