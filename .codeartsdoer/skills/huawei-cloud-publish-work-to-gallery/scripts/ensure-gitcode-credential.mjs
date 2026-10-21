#!/usr/bin/env node
// ensure-gitcode-credential.mjs — GitCode 凭证排查（一键）
//
// 背景:
//   SKILL.md Step 3 内联了 gitcode-oauth 登录前置逻辑，每次加载 SKILL.md 都付费。
//   本脚本封装为单入口，agent 只跑一条命令，按 stdout 决策。
//
// 用法:
//   node ensure-gitcode-credential.mjs
//
// 行为:
//   1. 检测本机已有 GitCode 凭证（git credential fill / ~/.git-credentials / $GITCODE_TOKEN / cmdkey / ~/.gitcode/auth.toml）
//   2. 有凭证 → exit 0，stdout "#credential=found source=<来源>"
//   3. 无凭证 → exit 0，stdout "#credential=missing"，指引执行 gitcode-oauth.ensure.mjs
//      （该脚本自带二进制自动安装：存在不下载，缺失自动装至用户级工具目录）
//
// 退出码: 0=有凭证或可走 OAuth 登录闭环

import { execSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import path from "node:path";

if (process.platform === "win32") {
  process.stdout.setDefaultEncoding?.("utf8");
  process.stderr.setDefaultEncoding?.("utf8");
}

const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  if (args[i] === "-h" || args[i] === "--help") {
    console.log(`ensure-gitcode-credential.mjs — GitCode 凭证排查

用法:
  node ensure-gitcode-credential.mjs

来源（#credential=found source=…）:
  git-credential-fill | git-credentials-file | env-GITCODE_TOKEN | cmdkey(Windows) | gitcode-auth-toml(OAuth)

退出码:
  0 = 有凭证（#credential=found）或无凭证可走 OAuth（#credential=missing）`);
    process.exit(0);
  }
}

function tryExec(cmd) {
  try { return execSync(cmd, { encoding: "utf8", timeout: 5000, stdio: ["ignore", "pipe", "pipe"] }).trim(); }
  catch { return ""; }
}

// `git credential fill` 的 protocol/host 经 stdin 喂入，不走 shell 内建 echo：
// dash（Debian/Ubuntu 的 /bin/sh）的 echo 不支持 -e，`echo -e "…" | git credential fill`
// 会把字面 "-e" 当成首行凭据属性喂给 git → 「已配置 credential helper 也检测不到凭证」。
// GIT_TERMINAL_PROMPT=0：helper 无凭证时禁止 git 打开 /dev/tty 交互索要用户名/口令（避免挂起到超时）。
function tryGitCredentialFill() {
  try {
    return execSync("git credential fill", {
      encoding: "utf8",
      timeout: 5000,
      stdio: ["pipe", "pipe", "ignore"],
      input: "protocol=https\nhost=gitcode.com\n\n",
      env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
    }).trim();
  } catch { return ""; }
}

// ---- 1. 检测本机已有凭证 ----
let credSource = "";

// git credential fill
const fillOut = tryGitCredentialFill();
if (fillOut && fillOut.includes("password=")) credSource = "git-credential-fill";

// ~/.git-credentials
if (!credSource) {
  const credFile = path.join(homedir(), ".git-credentials");
  if (existsSync(credFile)) {
    const raw = readFileSync(credFile, "utf8");
    if (raw.includes("gitcode.com")) credSource = "git-credentials-file";
  }
}

// $GITCODE_TOKEN
if (!credSource) {
  const tok = process.env.GITCODE_TOKEN;
  if (tok) credSource = "env-GITCODE_TOKEN";
}

// Windows cmdkey
if (!credSource && process.platform === "win32") {
  const cmdkeyOut = tryExec('cmdkey /list:git:https://gitcode.com 2>nul');
  if (cmdkeyOut) credSource = "cmdkey";
}

// ~/.gitcode/auth.toml（OAuth 登录后的 token 落盘点）
// 判定必须与 gitcode-oauth.ensure.mjs 的 main() 同源（剥 BOM 后须以 `access_token =` 开头），
// 否则会出现「本脚本报 #credential=missing、gitcode-oauth.ensure.mjs 报 #oauth=ready」的自相矛盾，
// 让 agent 多走一轮无意义的 OAuth 引导（Linux 无 cmdkey 时必现）。
// 路径与 gitcode-oauth.ensure.mjs 一致地尊重 GITCODE_HOME 覆盖。
if (!credSource) {
  const authFile = path.join(process.env.GITCODE_HOME || path.join(homedir(), ".gitcode"), "auth.toml");
  if (existsSync(authFile)) {
    try {
      const raw = readFileSync(authFile, "utf8").replace(/^\uFEFF/, "");
      if (/^access_token\s*=/.test(raw)) credSource = "gitcode-auth-toml";
    } catch { /* 不可读 → 视为无凭证 */ }
  }
}

if (credSource) {
  console.log(`#credential=found source=${credSource}`);
  process.exit(0);
}

// ---- 2. 无凭证 → 所有平台统一：仅 gitcode-oauth 扫码 / 点击链接授权登录 ----
// gitcode-oauth.ensure.mjs 自带二进制自动安装（存在不下载），此处不再生成安装命令。
// 登录闭环是两阶段（避免同步阻塞导致 agent 无法提示用户）：
//   ① --start   起 serve + 拿链接/二维码，立即返回 → agent 把链接和二维码一并原样展示给用户
//   ② --wait    用户授权完，轮询收尾写 token
console.log(`#credential=missing`);
console.error(`GitCode 凭证未检测到。执行两阶段 OAuth 登录：
  ① 阶段1（起 serve + 拿链接，立即返回）:
     node $SKILL_DIR/scripts/gitcode-oauth.ensure.mjs --start
     # stdout: #oauth=started session_id=<id> login_url=<url>；二维码走 stderr。
     # 把链接和二维码一并原样展示给用户，由用户在任意浏览器打开链接或扫码授权；禁止用 agent 内置浏览器代开。
  ② 阶段2（用户授权完成后轮询收尾）:
     node $SKILL_DIR/scripts/gitcode-oauth.ensure.mjs --wait <session_id>
     # 成功后 token 存 ~/.gitcode/auth.toml（stdout #oauth=done）。`);
process.exit(0);