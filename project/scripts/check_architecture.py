#!/usr/bin/env python3
"""Check the repository's currently established service and Atlas boundaries."""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SERVICE_ROOT = ROOT / "project" / "src" / "services"
ATLAS_ROOT = ROOT / "project" / "src" / "device_sides" / "atlas200idka2"
GO_MODULE = re.compile(r"^module\s+(\S+)", re.MULTILINE)
STRING = re.compile(r'"([^"\n]+)"')


def go_imports(source: str) -> list[str]:
    imports: list[str] = []
    lines = source.splitlines()
    index = 0
    while index < len(lines):
        line = lines[index].strip()
        if not line.startswith("import") or (len(line) > 6 and (line[6].isalnum() or line[6] == "_")):
            index += 1
            continue
        remainder = line[6:].strip()
        if remainder.startswith("("):
            index += 1
            while index < len(lines) and ")" not in lines[index]:
                imports.extend(STRING.findall(lines[index]))
                index += 1
            if index < len(lines):
                imports.extend(STRING.findall(lines[index]))
        else:
            imports.extend(STRING.findall(remainder))
        index += 1
    return imports


def check_go_modules(errors: list[str]) -> tuple[int, int]:
    modules: dict[str, Path] = {}
    for mod_file in SERVICE_ROOT.glob("*/go/go.mod"):
        match = GO_MODULE.search(mod_file.read_text(encoding="utf-8"))
        if not match:
            errors.append(f"{mod_file.relative_to(ROOT)}: missing module directive")
            continue
        modules[match.group(1)] = mod_file.parent.resolve()

    scanned_files = 0
    checked_imports = 0
    for module, module_root in modules.items():
        package_graph: dict[Path, set[Path]] = {}
        for source_file in module_root.rglob("*.go"):
            scanned_files += 1
            package_dir = source_file.parent.resolve()
            package_graph.setdefault(package_dir, set())
            for imported in go_imports(source_file.read_text(encoding="utf-8")):
                checked_imports += 1
                for other_module, other_root in modules.items():
                    if other_module != module and (imported == other_module or imported.startswith(other_module + "/")):
                        errors.append(
                            f"{source_file.relative_to(ROOT)} imports another service module {imported}"
                        )
                if imported == module or imported.startswith(module + "/"):
                    relative = imported[len(module):].lstrip("/")
                    dependency = (module_root / relative).resolve()
                    if dependency.is_dir() and dependency != package_dir:
                        package_graph[package_dir].add(dependency)
        detect_cycles(package_graph, errors, module_root)
    return scanned_files, checked_imports


def detect_cycles(graph: dict[Path, set[Path]], errors: list[str], module_root: Path) -> None:
    state: dict[Path, int] = {}
    stack: list[Path] = []
    reported: set[tuple[str, ...]] = set()

    def visit(node: Path) -> None:
        state[node] = 1
        stack.append(node)
        for dependency in graph.get(node, set()):
            if dependency not in graph:
                continue
            if state.get(dependency, 0) == 0:
                visit(dependency)
            elif state.get(dependency) == 1:
                cycle_nodes = stack[stack.index(dependency):] + [dependency]
                cycle = tuple(str(path.relative_to(module_root)).replace("\\", "/") for path in cycle_nodes)
                if cycle not in reported:
                    errors.append(f"Go package dependency cycle: {' -> '.join(cycle)}")
                    reported.add(cycle)
        stack.pop()
        state[node] = 2

    for node in graph:
        if state.get(node, 0) == 0:
            visit(node)


def check_atlas_application(errors: list[str]) -> int:
    roots = [ATLAS_ROOT / "src" / "application", ATLAS_ROOT / "include" / "application"]
    forbidden = ("adapters/", "manager/", "agent/", "common/core.hpp")
    scanned = 0
    for root in roots:
        for source_file in root.rglob("*"):
            if not source_file.is_file() or source_file.suffix not in {".cpp", ".hpp", ".h"}:
                continue
            scanned += 1
            for line_number, line in enumerate(source_file.read_text(encoding="utf-8").splitlines(), 1):
                if not line.lstrip().startswith("#include"):
                    continue
                include = line.replace("\\", "/")
                if any(part in include for part in forbidden):
                    errors.append(
                        f"{source_file.relative_to(ROOT)}:{line_number} application layer imports an adapter/runtime detail"
                    )
    return scanned


def main() -> int:
    errors: list[str] = []
    go_files, go_import_count = check_go_modules(errors)
    atlas_files = check_atlas_application(errors)
    if errors:
        print(f"FAIL: {len(errors)} architecture boundary violation(s)")
        for error in errors:
            print(f"  - {error}")
        return 1
    print("PASS: architecture dependency boundaries")
    print(f"  Go source files: {go_files}; imports checked: {go_import_count}")
    print(f"  Atlas application/port files: {atlas_files}")
    print("  Web feature-layer gate: pending T-15; no feature modules exist yet")
    return 0


if __name__ == "__main__":
    sys.exit(main())
