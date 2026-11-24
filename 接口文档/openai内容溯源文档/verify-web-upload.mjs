#!/usr/bin/env node
/**
 * verify-web-upload.mjs — 直接打 openai.com/verify 网页版溯源接口（批量 + 免前端限制）
 *
 * 网页版 https://openai.com/research/verify/ 的真实后端是：
 *   POST https://openai.com/verify/api/provenance_checks     (multipart: file)
 * 前端限制 = Cloudflare Turnstile 一次性 token（每次上传都要新 token）+ 单文件拖拽 + 无批量。
 *
 * 本脚本用本机已装的 Chrome（CDP，零依赖）做三件事：
 *   1) 打开页面，拿到 cf 会话 cookie + Turnstile 站点上下文
 *   2) 每次上传前 turnstile.reset(0)/execute(0) 现场换一个新 token（一次性）
 *   3) 在页面上下文里 fetch 上传（Origin/Referer/cookie/UA/指纹天然一致），
 *      不经过拖拽 UI，因此没有「一次一张」的限制
 *
 * 用法：
 *   node verify-web-upload.mjs az-out                      # 目录（不递归）
 *   node verify-web-upload.mjs -r modex-c2pa-out           # 递归
 *   node verify-web-upload.mjs a.png b.png --headless
 *   node verify-web-upload.mjs --dir . --limit 3 --keep-open
 *   node verify-web-upload.mjs --discover                  # 只抓页面真实请求，校核 endpoint
 *
 * 参数：
 *   --dir DIR | 位置参数      文件/目录
 *   -r, --recurse             递归
 *   --endpoint URL            默认 https://openai.com/verify/api/provenance_checks
 *   --page URL                默认 https://openai.com/research/verify/
 *   --headless                无头（Turnstile 更容易要求交互，成功率低）
 *   --port N                  CDP 端口，默认 9223
 *   --limit N                 只处理前 N 张
 *   --token-timeout MS        等 token 上限，默认 45000
 *   --out PATH                报告，默认 provenance-web-report.json
 *   --keep-open               跑完不关浏览器
 *   --node-post               改用 Node 侧 multipart 上传（页面只负责产 token/cookie）
 */

import fs from 'node:fs';
import fsp from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import { spawn } from 'node:child_process';

const MIME = { '.png': 'image/png', '.jpg': 'image/jpeg', '.jpeg': 'image/jpeg', '.webp': 'image/webp' };

function parseArgs(argv) {
  const o = {
    targets: [],
    dir: null,
    recurse: false,
    endpoint: 'https://openai.com/verify/api/provenance_checks',
    page: 'https://openai.com/research/verify/',
    headless: false,
    port: 9223,
    limit: 0,
    tokenTimeout: 45000,
    readyTimeout: 90000,
    maxAttempts: 4,
    backoff: 15000,
    delay: 3000,
    resume: true,
    out: 'provenance-web-report.json',
    keepOpen: false,
    nodePost: false,
    discover: false,
    chrome: process.env.CHROME_PATH || '',
  };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const next = () => argv[++i];
    if (a === '--dir') o.dir = next();
    else if (a === '-r' || a === '--recurse') o.recurse = true;
    else if (a === '--endpoint') o.endpoint = next();
    else if (a === '--page') o.page = next();
    else if (a === '--headless') o.headless = true;
    else if (a === '--port') o.port = Number(next()) || 9223;
    else if (a === '--limit') o.limit = Number(next()) || 0;
    else if (a === '--delay') o.delay = Math.max(0, Number(next()) || 0);
    else if (a === '--max-attempts') o.maxAttempts = Math.max(1, Number(next()) || 4);
    else if (a === '--backoff') o.backoff = Math.max(1000, Number(next()) || 15000);
    else if (a === '--no-resume') o.resume = false;
    else if (a === '--token-timeout') o.tokenTimeout = Number(next()) || 45000;
    else if (a === '--out') o.out = next();
    else if (a === '--keep-open') o.keepOpen = true;
    else if (a === '--node-post') o.nodePost = true;
    else if (a === '--discover') o.discover = true;
    else if (a === '--chrome') o.chrome = next();
    else if (a === '-h' || a === '--help') o.help = true;
    else if (!a.startsWith('--')) o.targets.push(a);
  }
  return o;
}

// ------------------------------------------------------------------ 极简 CDP
class CDP {
  constructor(ws) {
    this.ws = ws;
    this.seq = 0;
    this.pending = new Map();
    this.waiters = [];
    this.events = [];
    ws.addEventListener('message', (ev) => this.#onMessage(ev.data));
    ws.addEventListener('close', () => {
      for (const { reject } of this.pending.values()) reject(new Error('CDP 连接已关闭'));
      this.pending.clear();
    });
  }

  #onMessage(raw) {
    let msg;
    try {
      msg = JSON.parse(typeof raw === 'string' ? raw : Buffer.from(raw).toString('utf8'));
    } catch {
      return;
    }
    if (msg.id && this.pending.has(msg.id)) {
      const { resolve, reject, timer } = this.pending.get(msg.id);
      clearTimeout(timer);
      this.pending.delete(msg.id);
      if (msg.error) reject(new Error(`${msg.error.message || 'CDP error'} (${msg.error.code})`));
      else resolve(msg.result);
      return;
    }
    if (msg.method) {
      this.events.push(msg);
      if (this.events.length > 5000) this.events.shift();
      this.waiters = this.waiters.filter((w) => {
        if (w.method === msg.method && (!w.sessionId || w.sessionId === msg.sessionId)) {
          clearTimeout(w.timer);
          w.resolve(msg.params);
          return false;
        }
        return true;
      });
    }
  }

  send(method, params = {}, sessionId, timeoutMs = 60000) {
    const id = ++this.seq;
    const payload = { id, method, params };
    if (sessionId) payload.sessionId = sessionId;
    this.ws.send(JSON.stringify(payload));
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`CDP 超时: ${method}`));
      }, timeoutMs);
      this.pending.set(id, { resolve, reject, timer });
    });
  }

  waitEvent(method, sessionId, timeoutMs = 60000) {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waiters = this.waiters.filter((w) => w !== entry);
        reject(new Error(`等待事件超时: ${method}`));
      }, timeoutMs);
      const entry = { method, sessionId, resolve, timer };
      this.waiters.push(entry);
    });
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function findChrome(explicit) {
  const cands = [
    explicit,
    'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
    'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
  ].filter(Boolean);
  for (const p of cands) if (fs.existsSync(p)) return p;
  throw new Error('未找到 Chrome/Edge，可设置 CHROME_PATH 或用 --chrome 指定');
}

async function launchChrome(opt) {
  const exe = findChrome(opt.chrome);
  const profile = await fsp.mkdtemp(path.join(os.tmpdir(), 'prov-cdp-'));
  const args = [
    `--remote-debugging-port=${opt.port}`,
    `--user-data-dir=${profile}`,
    '--no-first-run',
    '--no-default-browser-check',
    '--disable-blink-features=AutomationControlled',
    '--disable-features=Translate,MediaRouter',
    '--window-size=1280,900',
  ];
  if (opt.headless) args.push('--headless=new');
  args.push('about:blank');
  const child = spawn(exe, args, { stdio: 'ignore', detached: false });
  const deadline = Date.now() + 30000;
  while (Date.now() < deadline) {
    try {
      const r = await fetch(`http://127.0.0.1:${opt.port}/json/version`);
      if (r.ok) {
        const v = await r.json();
        return { child, profile, wsUrl: v.webSocketDebuggerUrl, browser: v.Browser };
      }
    } catch {
      /* 还没起来 */
    }
    await sleep(250);
  }
  child.kill();
  throw new Error('Chrome CDP 端口未就绪');
}

// ------------------------------------------------------------------ 选择器
async function evalJs(cdp, sessionId, expression, { awaitPromise = false, timeout = 60000 } = {}) {
  const r = await cdp.send(
    'Runtime.evaluate',
    { expression, awaitPromise, returnByValue: true, allowUnsafeEvalBlockedByCSP: true },
    sessionId,
    timeout,
  );
  if (r.exceptionDetails) {
    throw new Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text || 'evaluate 失败');
  }
  return r.result?.value;
}

const TOKEN_EXPR = (timeoutMs) => `(async () => {
  const sel = 'input[name="cf-turnstile-response"]';
  const prev = document.querySelector(sel)?.value || '';
  const has = !!window.turnstile;
  if (has) {
    try { window.turnstile.reset(0); } catch (e) {}
    try { window.turnstile.execute(0); } catch (e) {}
  }
  const t0 = Date.now();
  while (Date.now() - t0 < ${timeoutMs}) {
    const v = document.querySelector(sel)?.value;
    if (v && v !== prev) return { token: v, hasTurnstile: has, minted: true };
    await new Promise((r) => setTimeout(r, 250));
  }
  const v = document.querySelector(sel)?.value || null;
  return {
    token: v,
    hasTurnstile: has,
    minted: !!v,
    widgets: document.querySelectorAll('.cf-turnstile, [data-turnstile], iframe[src*="challenges.cloudflare.com"]').length,
    title: document.title,
  };
})()`;

function inPageUploadExpr(b64, filename, mime, endpoint, token) {
  return `(async () => {
  const bin = atob(${JSON.stringify(b64)});
  const u8 = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
  const fd = new FormData();
  fd.append('file', new File([u8], ${JSON.stringify(filename)}, { type: ${JSON.stringify(mime)} }));
  const res = await fetch(${JSON.stringify(endpoint)}, {
    method: 'POST',
    body: fd,
    credentials: 'include',
    headers: { 'X-Turnstile-Token': ${JSON.stringify(token)} }
  });
  const text = await res.text();
  let json = null;
  try { json = JSON.parse(text); } catch (e) {}
  return { status: res.status, json, text: json ? null : text.slice(0, 600) };
})()`;
}

// ------------------------------------------------------------------ 文件收集
async function collect(targets, opt) {
  const roots = [...targets];
  if (opt.dir) roots.push(opt.dir);
  if (!roots.length) roots.push('.');
  const out = new Set();
  const walk = async (p) => {
    const st = await fsp.stat(p).catch(() => null);
    if (!st) return;
    if (st.isDirectory()) {
      for (const e of await fsp.readdir(p, { withFileTypes: true })) {
        if (e.name === 'node_modules' || e.name.startsWith('.git')) continue;
        const full = path.join(p, e.name);
        if (e.isDirectory()) {
          if (opt.recurse) await walk(full);
        } else if (MIME[path.extname(e.name).toLowerCase()]) out.add(full);
      }
    } else if (MIME[path.extname(p).toLowerCase()]) out.add(p);
  };
  for (const r of roots) await walk(r);
  return [...out].sort();
}

function normalize(body) {
  // 上游字段兼容：provenance_checks / content_provenance_check 两种命名都见过
  const results = body?.results || body?.result?.results || [];
  const c2pa = results.find((r) => (r.type || '').toLowerCase() === 'c2pa') || null;
  const synthid = results.find((r) => (r.type || '').toLowerCase() === 'synthid') || null;
  return {
    c2pa,
    synthid,
    detected: !!(c2pa?.outcome === 'detected' || synthid?.outcome === 'detected'),
  };
}

// ------------------------------------------------------------------ main
async function main() {
  const opt = parseArgs(process.argv.slice(2));
  if (opt.help) {
    console.log(fs.readFileSync(new URL(import.meta.url), 'utf8').split('*/')[0]);
    return;
  }
  const files = await collect(opt.targets, opt);
  if (opt.discover) {
    if (!files.length) console.log('（discover 模式不需要文件）');
  } else if (!files.length) {
    console.error('没找到 png/jpeg/webp 文件。');
    process.exit(1);
  }
  if (opt.limit) files.splice(opt.limit);

  const { child, profile, wsUrl, browser } = await launchChrome(opt);
  console.log(`Chrome: ${browser || 'ok'}  CDP: ${wsUrl}`);
  const ws = new WebSocket(wsUrl);
  await new Promise((res, rej) => {
    ws.addEventListener('open', res, { once: true });
    ws.addEventListener('error', () => rej(new Error('WebSocket 连接失败')), { once: true });
  });
  const cdp = new CDP(ws);

  const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true });
  await cdp.send('Page.enable', {}, sessionId);
  await cdp.send('Runtime.enable', {}, sessionId);
  await cdp.send('Network.enable', {}, sessionId);

  const requests = new Set();
  cdp.waitEvent('Network.requestWillBeSent', sessionId, 300000).catch(() => {});
  setInterval(() => {
    for (const ev of cdp.events) {
      if (ev.method === 'Network.requestWillBeSent' && ev.params?.request?.url) {
        const u = ev.params.request.url;
        if (/provenance|verify/i.test(u)) requests.add(`${ev.params.request.method} ${u}`);
      }
    }
  }, 500).unref();

  console.log(`打开: ${opt.page}`);
  await cdp.send('Page.navigate', { url: opt.page }, sessionId, 90000);
  await sleep(3000);

  // 等 Turnstile 前端就绪：window.turnstile + 隐藏 input 或 challenge iframe
  const READY_EXPR = `({
    turnstile: !!window.turnstile,
    cfInput: !!document.querySelector('input[name="cf-turnstile-response"]'),
    widgets: document.querySelectorAll('.cf-turnstile, [data-turnstile], iframe[src*="challenges.cloudflare.com"]').length,
    bodyLen: document.body ? document.body.innerText.length : 0
  })`;
  let probe = null;
  const readyDeadline = Date.now() + (opt.readyTimeout || 90000);
  let tries = 0;
  while (Date.now() < readyDeadline) {
    probe = await evalJs(cdp, sessionId, READY_EXPR, {});
    if (probe && probe.turnstile && (probe.cfInput || probe.widgets > 0)) break;
    // 触发懒加载：滚动 + 轻微鼠标移动事件
    if (tries % 6 === 5) {
      await evalJs(cdp, sessionId, `(() => { window.scrollBy(0, 300); window.scrollBy(0, -300);
        document.dispatchEvent(new MouseEvent('mousemove', { clientX: 400, clientY: 400, bubbles: true })); return 1; })()`, {});
    }
    tries++;
    await sleep(1000);
  }
  probe = await evalJs(cdp, sessionId, `${READY_EXPR.slice(0, -2)}, url: location.href, title: document.title })`, {});
  console.log(`页面状态(等待 ${tries}s):`, JSON.stringify(probe));
  if (!probe?.turnstile) {
    console.log('Turnstile 脚本没起来：可能被 CF 拦截 / 需要--headless 去掉 / 或该环境 IP 被挑战。仍继续尝试上传前再取 token。');
  }
  await sleep(1500);

  if (opt.discover) {
    await sleep(8000);
    console.log('\n捕获到的相关请求：');
    for (const r of [...requests].sort()) console.log('  ' + r);
    console.log('\n（若上面没有 /verify/api/provenance_checks，说明 endpoint 改名了，用 --endpoint 指定实际地址）');
    if (!opt.keepOpen) {
      ws.close();
      child.kill();
      await fsp.rm(profile, { recursive: true, force: true }).catch(() => {});
    }
    process.exit(0);
  }

  // 断点续跑：读旧报告，跳过已 status=200 的文件
  const previous = new Map();
  if (opt.resume && fs.existsSync(opt.out)) {
    try {
      const old = JSON.parse(await fsp.readFile(opt.out, 'utf8'));
      for (const r of old.results || []) if (r.status === 200) previous.set(r.file, r);
      if (previous.size) console.log(`续跑：${previous.size} 个已有结果将复用\n`);
    } catch {
      /* 旧报告坏了就当没有 */
    }
  }

  const mintToken = async () => {
    const t = await evalJs(cdp, sessionId, TOKEN_EXPR(opt.tokenTimeout), { awaitPromise: true, timeout: opt.tokenTimeout + 15000 });
    return t?.token || null;
  };

  const postOnce = async (file, buf, mime) => {
    const token = await mintToken();
    if (!token) return { status: 0, body: { error: 'no turnstile token' } };
    if (opt.nodePost) {
      const cookies = await cdp.send('Network.getCookies', { urls: [opt.page, opt.endpoint] }, sessionId);
      const cookieHeader = (cookies.cookies || []).map((c) => `${c.name}=${c.value}`).join('; ');
      const fd = new FormData();
      fd.append('file', new Blob([buf], { type: mime }), path.basename(file));
      const res = await fetch(opt.endpoint, {
        method: 'POST',
        headers: {
          Origin: new URL(opt.page).origin,
          Referer: opt.page,
          'X-Turnstile-Token': token,
          Cookie: cookieHeader,
        },
        body: fd,
      });
      const text = await res.text();
      let body = null;
      try {
        body = JSON.parse(text);
      } catch {
        body = { raw: text.slice(0, 600) };
      }
      return { status: res.status, body };
    }
    const r = await evalJs(cdp, sessionId, inPageUploadExpr(buf.toString('base64'), path.basename(file), mime, opt.endpoint, token), {
      awaitPromise: true,
      timeout: 180000,
    });
    return { status: r?.status ?? 0, body: r?.json ?? r?.text ?? null };
  };

  // 429 是 CF 按 IP 的严格限流：指数退避 + 每次重新签 token
  const rows = [];
  let cooldownUntil = 0;
  for (let i = 0; i < files.length; i++) {
    const file = files[i];
    const rel = path.relative(process.cwd(), file) || file;
    const tag = `[${String(i + 1).padStart(3)}/${files.length}]`;

    if (previous.has(rel)) {
      const p = previous.get(rel);
      rows.push(p);
      console.log(`${tag} cached C2PA=${p.c2pa?.outcome || '-'}/${p.c2pa?.validation_state || '-'} SynthID=${p.synthid?.outcome || '-'}   ${rel}`);
      continue;
    }

    const buf = await fsp.readFile(file);
    const mime = MIME[path.extname(file).toLowerCase()] || 'application/octet-stream';

    let status = 0;
    let body = null;
    for (let attempt = 1; attempt <= opt.maxAttempts; attempt++) {
      const wait = cooldownUntil - Date.now();
      if (wait > 0) await sleep(wait);
      try {
        const r = await postOnce(file, buf, mime);
        status = r.status;
        body = r.body;
      } catch (e) {
        status = 0;
        body = { error: e.message };
      }
      if (status === 200) break;
      const retriable = status === 429 || status === 0 || status >= 500;
      if (!retriable || attempt === opt.maxAttempts) break;
      const backoff = opt.backoff * Math.pow(2, attempt - 1) * (1 + Math.random() * 0.3);
      cooldownUntil = Date.now() + backoff;
      console.log(`${tag} ${status || 'net'} 重试 ${attempt}/${opt.maxAttempts}，退避 ${Math.round(backoff / 1000)}s  ${rel}`);
    }

    const norm = normalize(body);
    const line =
      status === 200
        ? `C2PA=${norm.c2pa?.outcome || '-'}/${norm.c2pa?.validation_state || '-'} SynthID=${norm.synthid?.outcome || '-'}`
        : `HTTP ${status} ${JSON.stringify(body).slice(0, 90)}`;
    console.log(`${tag} ${line.slice(0, 100).padEnd(44)} ${rel}`);
    rows.push({
      file: rel,
      bytes: buf.length,
      status,
      openai_detected: norm.detected,
      c2pa: norm.c2pa,
      synthid: norm.synthid,
      upstream: status === 200 ? body : null,
      error: status === 200 ? undefined : body,
    });
    if (status === 200) await sleep(opt.delay);
  }

  // 把上一轮里、本次不在处理范围内的历史结果合并回来，避免报告丢字段
  for (const [k, v] of previous) if (!rows.some((r) => r.file === k)) rows.push(v);
  rows.sort((a, b) => String(a.file).localeCompare(String(b.file)));

  const report = {
    generated_at: new Date().toISOString(),
    endpoint: opt.endpoint,
    page: opt.page,
    summary: {
      total: rows.length,
      ok: rows.filter((r) => r.status === 200).length,
      openai_detected: rows.filter((r) => r.openai_detected).length,
      error: rows.filter((r) => r.status !== 200).length,
    },
    discovered_requests: [...requests].sort(),
    results: rows,
  };
  await fsp.writeFile(opt.out, JSON.stringify(report, null, 2), 'utf8');

  console.log('\n================ 汇总 ================');
  const pad = (s, n) => String(s ?? '-').padEnd(n).slice(0, n);
  console.log(`${pad('file', 56)}${pad('c2pa', 13)}${pad('state', 13)}${pad('synthid', 12)}model`);
  for (const r of rows.filter((x) => x.status === 200)) {
    console.log(
      `${pad(r.file, 56)}${pad(r.c2pa?.outcome, 13)}${pad(r.c2pa?.validation_state, 13)}${pad(r.synthid?.outcome, 12)}${r.c2pa?.model ?? r.synthid?.model ?? '-'}`,
    );
  }
  for (const r of rows.filter((x) => x.status !== 200)) {
    console.log(`ERR ${r.file}  ${r.status}  ${JSON.stringify(r.error).slice(0, 200)}`);
  }
  console.log(`\n报告: ${path.resolve(opt.out)}`);
  if (requests.size) console.log(`页面真实请求样本: ${[...requests].slice(0, 5).join(' | ')}`);

  if (!opt.keepOpen) {
    ws.close();
    child.kill();
    await sleep(500);
    await fsp.rm(profile, { recursive: true, force: true }).catch(() => {});
  } else {
    console.log('浏览器保留（--keep-open），端口 ' + opt.port);
  }
  process.exit(rows.some((r) => r.status !== 200) ? 1 : 0);
}

main().catch((e) => {
  console.error('fatal:', e.message);
  process.exit(1);
});
