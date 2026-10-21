#!/usr/bin/env node
// spawn-detached.mjs — 启动独立进程（脱离 bash 工具 job-object 清理）
// 用法: node spawn-detached.mjs --out <file> [--pidfile <file>] -- <cmd> <args...>
// 契约: stdout `#spawn=ok pid=<pid> out=<file>` | `#spawn=failed reason=<...>`
import { spawnSync } from "node:child_process";
import { writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const argv = process.argv.slice(2);

const HELP = `spawn-detached.mjs — 脱管启动子进程（脱离工具 job-object 清理，进程长期存活）
用法: node spawn-detached.mjs --out <file> [--pidfile <file>] [--cwd <dir>] -- <cmd> <args...>
选项:
  --out <file>      子进程 stdout+stderr 的重定向目标（必填；父目录自动创建）
  --pidfile <file>  写入子进程 PID
  --cwd <dir>       子进程工作目录（默认取 process.cwd()；**必须传 workDir**，
                    否则 http-server 会 serve 错目录导致截图空白）
  -- <cmd> <args>   \`--\` 之后要启动的命令（优先入口脚本，如 npm run dev / python main.py）
行为: **立即返回，不等服务就绪** —— 调用方必须自行轮询健康检查
      （如 curl -sf --max-time 3 http://localhost:<port>）。
      win32 走 WMI Win32_Process.Create；POSIX 走 setsid。
退出码: 0=已启动; 1=启动失败; 2=参数错误`;
if (argv.some((a) => a === "-h" || a === "--help")) {
  console.log(HELP);
  process.exit(0);
}
const sep = argv.indexOf("--");
if (sep === -1) { console.error("用法: spawn-detached.mjs --out <file> [--pidfile <file>] [--cwd <dir>] -- <cmd> <args...>"); process.exit(2); }
let outFile = "", pidFile = "", cwdArg = "";
const cmd = argv.slice(sep + 1);
for (let i = 0; i < sep; i++) {
  if (argv[i] === "--out") outFile = argv[++i];
  else if (argv[i] === "--pidfile") pidFile = argv[++i];
  else if (argv[i] === "--cwd") cwdArg = argv[++i];
}
if (!outFile || !cmd.length) { console.error("用法: spawn-detached.mjs --out <file> [--pidfile <file>] [--cwd <dir>] -- <cmd> <args...>"); process.exit(2); }
const cwd = cwdArg || process.cwd();
mkdirSync(dirname(outFile), { recursive: true });
if (pidFile) mkdirSync(dirname(pidFile), { recursive: true });

const die = (m) => { console.log(`#spawn=failed reason=${m}`); process.exit(1); };

if (process.platform === "win32") {
  // 解析裸命令为真实可执行路径：cmd 中带引号的裸名（"npm"）会绕过 PATHEXT，
  // 导致命中 node_modules 里的同名文件/目录而报 MODULE_NOT_FOUND（如 npm-cli.js）。
  const resolveWinCmd = (name) => {
    if (/[/\\]/.test(name) || /\.(exe|cmd|bat|com)$/i.test(name)) return name;
    const r = spawnSync("where.exe", [name], { encoding: "utf8" });
    if (r.status !== 0) return name;
    const lines = r.stdout.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
    return lines.find((l) => /\.(exe|cmd|bat|com)$/i.test(l)) || lines[0] || name;
  };
  const cmd0 = resolveWinCmd(cmd[0]);
  const wrapperPath = join(tmpdir(), `spawn-wrapper-${Date.now()}.cmd`);
  const argsStr = cmd.slice(1).map(a => a.includes(" ") || a.includes('"') ? `"${a}"` : a).join(" ");
  writeFileSync(wrapperPath, `@echo off\r\n"${cmd0}" ${argsStr} > "${outFile}" 2>&1\r\n`);
  const wmiCmd = `cmd /c "${wrapperPath}"`;
  const ps = [
    `$cmd = @'`,
    wmiCmd,
    `'@`,
    `$cwd = @'`,
    cwd,
    `'@`,
    `$p=([wmiclass]"Win32_Process").Create($cmd, $cwd)`,
    `if($p.ReturnValue -ne 0){Write-Output "ERROR:$($p.ReturnValue)"}else{Write-Output $p.ProcessId}`,
  ].join("\n");
  const r = spawnSync("powershell", ["-NoProfile", "-NonInteractive", "-Command", ps], { encoding: "utf8", timeout: 15000, stdio: ["ignore", "pipe", "pipe"] });
  if (r.status !== 0) die(`wmi-failed: ${r.stderr.trim() || r.stdout.trim()}`);
  const out = r.stdout.trim();
  if (out.startsWith("ERROR:")) die(`wmi-return=${out.slice(6)}`);
  if (!/^\d+$/.test(out)) die(`invalid-pid: ${out}`);
  if (pidFile) try { writeFileSync(pidFile, out); } catch {}
  console.log(`#spawn=ok pid=${out} out=${outFile}`);
} else {
  const esc = (s) => s.replace(/'/g, "'\\''");
  const exeArgs = cmd.slice(1).map(a => `'${esc(a)}'`).join(" ");
  const pidCmd = pidFile ? `echo \\$\\$ > '${esc(pidFile)}'; ` : "";
  const cdCmd = `cd '${esc(cwd)}'; `;
  const inner = `${cdCmd}${pidCmd}exec '${esc(cmd[0])}' ${exeArgs} > '${esc(outFile)}' 2>&1`;
  const r = spawnSync("bash", ["-c", `setsid bash -c '${inner.replace(/'/g, "'\\''")}' < /dev/null > /dev/null 2>&1 & echo $!`], { encoding: "utf8", timeout: 10000, stdio: ["ignore", "pipe", "pipe"] });
  if (r.status !== 0) die(`setsid-failed: ${r.stderr.trim()}`);
  const pid = r.stdout.trim();
  if (!/^\d+$/.test(pid)) die(`invalid-pid: ${pid}`);
  for (let i = 0; i < 10 && pidFile && !existsSync(pidFile); i++) spawnSync("sleep", ["0.1"], { stdio: "ignore" });
  console.log(`#spawn=ok pid=${pid} out=${outFile}`);
}
