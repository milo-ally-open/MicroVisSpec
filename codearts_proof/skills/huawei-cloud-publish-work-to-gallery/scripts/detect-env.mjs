#!/usr/bin/env node
// detect-env.mjs — 输出一次运行环境的平台指纹，SKILL.md 据此分流只读对应平台信息
//
// 用法:
//   node detect-env.mjs           # stdout = JSON，如 {"os":"linux","shell":"bash","distro":"rhel","sandbox":false,"tunnel":true}
//
// 字段:
//   os      win32 | linux | darwin      （process.platform）
//   shell   powershell | git-bash | bash
//   distro  linux 下: rhel(含 EulerOS/HCE/CentOS) | debian | other；其他平台 n/a
//   sandbox linux 下探测 CodeArts 沙箱（127.0.0.1:19300/healthz == ok）
//   tunnel  true=可开 DevBridge 隧道（仅 linux）；false=无隧道能力（win32/darwin）→ envUrl="" 跳过隧道
//
// 使用约定:
//   - win32 → 按 fingerprint-shell 查看定平台文件（work-preparation-windows.md）；跳过 Linux/EulerOS 小节；tunnel=false
//   - linux  → 看 work-preparation-linux.md；sandbox=true 再读 codearts-sandbox.md；跳过 Windows 小节；tunnel=true

import { execFileSync } from "node:child_process";

const HELP = `detect-env.mjs — 输出运行环境指纹（stdout 单行 JSON），SKILL.md 据此只读对应平台做法
用法: node detect-env.mjs [-h|--help]（无其它参数）
字段:
  os      win32 | linux | darwin
  shell   powershell | git-bash | bash
  distro  linux: rhel | debian | other；非 linux 为 n/a
  sandbox linux 探测 CodeArts 沙箱（127.0.0.1:19300/healthz）
  tunnel  true = 可开 DevBridge 隧道（仅 linux）→ 询问用户是否开隧道；
          false（win32/darwin）→ 禁止尝试开隧道，envUrl="" 直接照常发布
行为: 瞬时返回；非 linux 分支不发子进程、无网络请求（linux 分支读 /etc/os-release 并探测本地端口）
退出码: 0=成功; 2=参数错误`;
if (process.argv.slice(2).some((a) => a === "-h" || a === "--help")) {
  console.log(HELP);
  process.exit(0);
}
const unknown = process.argv.slice(2);
if (unknown.length) {
  console.error(`❌ detect-env.mjs 不接受参数，收到: ${unknown.join(" ")}\n  运行 node detect-env.mjs --help 查看用法。`);
  process.exit(2);
}

function sh(cmd, args, timeout = 3000) {
  try {
    return execFileSync(cmd, args, { encoding: "utf8", timeout, stdio: ["ignore", "pipe", "ignore"] }).trim();
  } catch {
    return "";
  }
}

const os = process.platform; // win32 | linux | darwin
let shell = "bash";
let distro = "n/a";
let sandbox = false;
const tunnel = os === "linux"; // DevBridge 隧道仅 Linux 目标环境可用；Windows/macOS → envUrl=""

if (os === "win32") {
  // PowerShell 宿主 vs Git Bash / MSYS2（preflight 按 shell 走 sh 或 ps1）
  const msys = /MSYSTEM|GIT_BASH/i.test(process.env.MSYSTEM || "") ||
    /bash|mintty/i.test(process.env.SHELL || "");
  shell = msys ? "git-bash" : "powershell";
} else if (os === "darwin") {
  shell = "bash";
} else {
  shell = "bash";
  const rel = sh("bash", ["-c", "grep -E '^(ID|ID_LIKE|NAME)=' /etc/os-release 2>/dev/null"]);
  distro = /(^|\n)ID=(hce|euler|centos|rhel)/i.test(rel) || /ID_LIKE=.*(rhel|fedora|centos)/i.test(rel)
    ? "rhel"
    : /ID=debian|ID=ubuntu|ID=fedora|ID_LIKE=.*debian/i.test(rel)
      ? "debian"
      : "other";
  sandbox = /ok/i.test(sh("curl", ["--max-time", "3", "-s", "http://127.0.0.1:19300/healthz"]));
}

process.stdout.write(`${JSON.stringify({ os, shell, distro, sandbox, tunnel })}\n`);