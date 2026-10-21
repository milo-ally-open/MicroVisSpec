#!/usr/bin/env node
// check-version.mjs — 单命令检查技能是否过时（零依赖，内部调用 api.mjs）。
// 读取本技能 SKILL.md frontmatter 的 name 与本地 version（兼容顶层 `version:` 与嵌套 `metadata.version:`），
// 调平台 open-api-guest /v1/gallery/skills/status?name=<本技能名>&version=<本地版本> 查询状态：
//   pass / skip → 不拦截（status=ok / status=skip，exit 0）；outdated → status=outdated + 平台下发升级文案，exit 1。
// 判定逻辑（含版本比较、已登记/未登记、version 缺失）统一在平台侧；
// 平台不可达/响应异常 → status=skip（尽力而为，不阻塞发布）。
import fs from "node:fs";
import path from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

if (process.platform === "win32") {
  process.stdout.setDefaultEncoding?.("utf8");
  process.stderr.setDefaultEncoding?.("utf8");
}

const HELP = `check-version.mjs — 检查本技能是否已被平台判为过时（零参数、零依赖）
用法: node check-version.mjs [-h|--help]
行为: 读本技能 SKILL.md frontmatter 的 name/version，网络请求 /v1/gallery/skills/status 判定（6s 超时）；
      查询失败或平台不可达 → 降级 status=skip 并在 stderr 告警，不阻塞发布。
      版本比较、已登记/未登记、version 缺失的判定统一在平台侧。
输出: status=ok（版本最新）| status=skip（未登记或无法比对）| status=outdated（附平台升级文案）
退出码: 0=不拦截（ok/skip）; 1=outdated，必须原样提示用户升级并停止; 2=参数错误`;
if (process.argv.slice(2).some((a) => a === "-h" || a === "--help")) {
  console.log(HELP);
  process.exit(0);
}
const unknown = process.argv.slice(2);
if (unknown.length) {
  console.error(`❌ check-version.mjs 不接受参数，收到: ${unknown.join(" ")}\n  运行 node check-version.mjs --help 查看用法。`);
  process.exit(2);
}

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const skillRoot = path.resolve(__dirname, "..");

function frontmatter() {
  try {
    const md = fs.readFileSync(path.join(skillRoot, "SKILL.md"), "utf8");
    const m = md.match(/^---\r?\n([\s\S]*?)\r?\n---/);
    return m ? m[1] : "";
  } catch {
    return "";
  }
}

function frontmatterValue(block, key) {
  const m = block.match(new RegExp(`^${key}\\s*:\\s*(.+?)\\s*$`, "m"));
  return m ? m[1].trim() : "";
}

// 本地版本：兼容顶层 `version:` 与嵌套 `metadata.version:`（缩进不定，取首个匹配）。
function frontmatterVersion(block) {
  const m = block.match(/^\s*version\s*:\s*(.+?)\s*$/m);
  return m ? m[1].trim() : "";
}

function runApiGet(args) {
  try {
    const out = execFileSync(process.execPath, [path.join(__dirname, "api.mjs"), "GET", ...args], {
      encoding: "utf8",
      timeout: 6000,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let status = 0;
    let data = null;
    for (const raw of String(out).split(/\r?\n/)) {
      if (status === 0 && raw.startsWith("#status=")) {
        status = Number(raw.slice(8)) || 0;
      } else {
        const t = raw.trim();
        if (!data && t.startsWith("{")) {
          try { data = JSON.parse(t); } catch { data = null; }
        }
      }
    }
    return { status, data };
  } catch {
    return { status: 0, data: null };
  }
}

const fm = frontmatter();
const skillName = frontmatterValue(fm, "name") || "publish-work-to-gallery";
const local = frontmatterVersion(fm);

// 本地 version 缺失/无法解析时按空串上报：已登记技能由平台判为 outdated（极老技能强制升级）；未登记技能平台返回 skip。
const statusRes = runApiGet(["/v1/gallery/skills/status", "--prefix", "open-api-guest", "--query", `name=${skillName}&version=${local}`]);
const status = typeof statusRes.data?.data?.status === "string" ? statusRes.data.data.status : null;

if (status === "pass") {
  console.log("status=ok");
  process.exit(0);
}
if (status === "skip") {
  console.log("status=skip");
  process.exit(0);
}
if (status === "outdated") {
  let prompt = typeof statusRes.data?.data?.prompt === "string" ? statusRes.data.data.prompt.trim() : "";
  if (!prompt) {
    // 兜底：响应未带文案时经 /prompt 拉取 skills|outdated（按本技能名渲染）。
    const promptRes = runApiGet(["/v1/gallery/prompt", "--prefix", "open-api-guest", "--query", `type=skills&target=outdated&params=${encodeURIComponent(JSON.stringify({ skillName }))}`]);
    prompt = typeof promptRes.data?.data?.prompt === "string" ? promptRes.data.data.prompt.trim() : "";
  }
  console.log("status=outdated");
  if (prompt) console.log(prompt);
  process.exit(1);
}
// 平台不可达/响应异常 → 尽力而为（不阻塞）。但必须显式告警：
// 查询本身失败时静默输出 skip，会把「技能已过时」这类本该拦截的判定一并吞掉。
if (statusRes.status !== 200) {
  console.error(`⚠️ 技能版本查询未成功（#status=${statusRes.status}），已按 status=skip 放行，本次未做版本比对。`);
  console.error(`   排查：确认 GALLERY_API_HOST 可达，且当前环境允许创建子进程（受限沙箱下管道式捕获会 EPERM）。`);
}
console.log("status=skip");
process.exit(0);