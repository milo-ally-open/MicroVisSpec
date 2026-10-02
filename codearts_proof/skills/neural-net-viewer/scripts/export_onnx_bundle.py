#!/usr/bin/env python3
"""Export a Netron SVG/PNG and an ONNX graph manifest with cross-referenced nodes."""

from __future__ import annotations

import argparse
import copy
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any


SCHEMA_VERSION = "1.0"


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def json_safe(value: Any, *, max_array_items: int = 256) -> Any:
    """Convert ONNX attribute values to JSON without dumping large arrays."""
    try:
        import numpy as np
        if isinstance(value, np.ndarray):
            if value.size <= max_array_items:
                return value.tolist()
            raw = value.tobytes(order="C")
            return {
                "shape": list(value.shape),
                "dtype": str(value.dtype),
                "sha256": hashlib.sha256(raw).hexdigest(),
                "omitted_values": int(value.size),
            }
        if isinstance(value, np.generic):
            return value.item()
    except ImportError:
        pass

    if isinstance(value, bytes):
        try:
            return value.decode("utf-8")
        except UnicodeDecodeError:
            return {"encoding": "base64", "data": __import__("base64").b64encode(value).decode("ascii")}
    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    if isinstance(value, (list, tuple)):
        return [json_safe(item, max_array_items=max_array_items) for item in value]
    if hasattr(value, "DESCRIPTOR"):
        try:
            from google.protobuf.json_format import MessageToDict
            return MessageToDict(value, preserving_proto_field_name=True)
        except Exception:
            return {"protobuf_type": value.DESCRIPTOR.full_name, "text": str(value)}
    return str(value)


def shape_json(value_info: Any, onnx: Any) -> dict[str, Any]:
    result: dict[str, Any] = {"name": value_info.name}
    if value_info.doc_string:
        result["doc_string"] = value_info.doc_string
    type_proto = value_info.type
    if not type_proto.WhichOneof("value"):
        result["type"] = "unknown"
        return result
    kind = type_proto.WhichOneof("value")
    result["kind"] = kind
    if kind == "tensor_type":
        tensor_type = type_proto.tensor_type
        try:
            result["dtype"] = onnx.TensorProto.DataType.Name(tensor_type.elem_type)
        except ValueError:
            result["dtype"] = f"UNDEFINED({tensor_type.elem_type})"
        if tensor_type.HasField("shape"):
            dims = []
            for dim in tensor_type.shape.dim:
                if dim.HasField("dim_value"):
                    dims.append(dim.dim_value)
                elif dim.HasField("dim_param"):
                    dims.append({"symbol": dim.dim_param})
                else:
                    dims.append(None)
            result["shape"] = dims
        else:
            result["shape"] = None
    elif kind == "sparse_tensor_type":
        sparse_type = type_proto.sparse_tensor_type
        try:
            result["dtype"] = onnx.TensorProto.DataType.Name(sparse_type.elem_type)
        except ValueError:
            result["dtype"] = f"UNDEFINED({sparse_type.elem_type})"
        result["shape"] = [dim.dim_value if dim.HasField("dim_value") else ({"symbol": dim.dim_param} if dim.HasField("dim_param") else None) for dim in sparse_type.shape.dim]
    else:
        result["type_details"] = str(type_proto)
    return result


def tensor_json(tensor: Any, onnx: Any, source_dir: Path, external_files: dict[str, dict[str, Any]]) -> dict[str, Any]:
    try:
        dtype = onnx.TensorProto.DataType.Name(tensor.data_type)
    except ValueError:
        dtype = f"UNDEFINED({tensor.data_type})"
    item: dict[str, Any] = {
        "name": tensor.name,
        "dtype": dtype,
        "shape": list(tensor.dims),
        "data_location": "EXTERNAL" if tensor.data_location == onnx.TensorProto.EXTERNAL else "INLINE",
        "protobuf_sha256": hashlib.sha256(tensor.SerializeToString(deterministic=True)).hexdigest(),
    }
    if tensor.external_data:
        refs = {}
        for entry in tensor.external_data:
            refs[entry.key] = entry.value
        item["external_data"] = refs
        location = refs.get("location")
        if location:
            resolved = (source_dir / location).resolve()
            key = str(resolved)
            if key not in external_files:
                try:
                    resolved.relative_to(source_dir.resolve())
                    inside_model_dir = True
                except ValueError:
                    inside_model_dir = False
                if not inside_model_dir:
                    external_files[key] = {"path": location, "status": "outside_model_directory_not_hashed"}
                elif resolved.is_file():
                    external_files[key] = {"path": location, "sha256": sha256_file(resolved), "size_bytes": resolved.stat().st_size}
                else:
                    external_files[key] = {"path": location, "status": "missing"}
            item["external_file"] = external_files[key]
    elif tensor.raw_data:
        item["data_sha256"] = hashlib.sha256(tensor.raw_data).hexdigest()
    else:
        # Typed fields are already represented by the deterministic protobuf hash.
        item["typed_data_present"] = bool(tensor.float_data or tensor.int32_data or tensor.string_data or tensor.int64_data or tensor.double_data or tensor.uint64_data)
    return item


def attribute_json(attribute: Any, graph_id: str, node_id: str, graph_builder: Any, onnx: Any, source_dir: Path, external_files: dict[str, dict[str, Any]]) -> dict[str, Any]:
    item: dict[str, Any] = {"name": attribute.name, "type": onnx.AttributeProto.AttributeType.Name(attribute.type)}
    kind = attribute.WhichOneof("value")
    if kind in ("g", "graphs"):
        graphs = [attribute.g] if kind == "g" else list(attribute.graphs)
        graph_ids = []
        for index, subgraph in enumerate(graphs):
            sub_id = f"{graph_id}/node/{node_id.rsplit('/', 1)[-1]}/attr/{attribute.name or 'unnamed'}"
            if len(graphs) > 1:
                sub_id += f"/{index:04d}"
            graph_builder(subgraph, sub_id)
            graph_ids.append(sub_id)
        item["graph_ids"] = graph_ids
    elif kind in ("t", "tensors"):
        tensors = [attribute.t] if kind == "t" else list(attribute.tensors)
        item["tensors"] = [tensor_json(tensor, onnx, source_dir, external_files) for tensor in tensors]
    elif kind == "sparse_tensor":
        item["sparse_tensor"] = {
            "values": tensor_json(attribute.sparse_tensor.values, onnx, source_dir, external_files),
            "indices": tensor_json(attribute.sparse_tensor.indices, onnx, source_dir, external_files),
        }
    elif kind == "sparse_tensors":
        item["sparse_tensors"] = [
            {"values": tensor_json(t.values, onnx, source_dir, external_files), "indices": tensor_json(t.indices, onnx, source_dir, external_files)}
            for t in attribute.sparse_tensors
        ]
    elif kind:
        item["value"] = json_safe(getattr(attribute, kind))
    return item


def build_graph(graph: Any, graph_id: str, onnx: Any, source_dir: Path, external_files: dict[str, dict[str, Any]], visual_ids: dict[str, str] | None, visual_counter: list[int]) -> dict[str, Any]:
    subgraphs: list[dict[str, Any]] = []

    def add_subgraph(subgraph: Any, subgraph_id: str) -> None:
        subgraphs.append(build_graph(subgraph, subgraph_id, onnx, source_dir, external_files, visual_ids, visual_counter))

    graph_result: dict[str, Any] = {
        "id": graph_id,
        "name": graph.name,
        "doc_string": graph.doc_string,
        "metadata_props": {item.key: item.value for item in getattr(graph, "metadata_props", [])},
        "inputs": [shape_json(value, onnx) for value in graph.input],
        "outputs": [shape_json(value, onnx) for value in graph.output],
        "value_info": [shape_json(value, onnx) for value in graph.value_info],
        "initializers": [tensor_json(tensor, onnx, source_dir, external_files) for tensor in graph.initializer],
        "sparse_initializers": [
            {"values": tensor_json(t.values, onnx, source_dir, external_files), "indices": tensor_json(t.indices, onnx, source_dir, external_files)}
            for t in graph.sparse_initializer
        ],
        "quantization_annotations": [
            {"tensor_name": annotation.tensor_name, "quant_parameter_tensor_names": {item.key: item.value for item in annotation.quant_parameter_tensor_names}}
            for annotation in getattr(graph, "quantization_annotation", [])
        ],
        "nodes": [],
        "tensors": [],
    }
    producers: dict[str, str] = {}
    consumers: dict[str, list[str]] = {}
    for index, node in enumerate(graph.node):
        node_id = f"{graph_id}/node/{index:06d}"
        visual_id = None
        if visual_ids is not None:
            visual_counter[0] += 1
            visual_id = f"v{visual_counter[0]:07d}"
            visual_ids[node_id] = visual_id
        node_result = {
            "id": node_id,
            "visual_id": visual_id,
            "index": index,
            "name": node.name,
            "domain": node.domain or "ai.onnx",
            "op_type": node.op_type,
            "overload": getattr(node, "overload", ""),
            "doc_string": node.doc_string,
            "inputs": [{"name": name, "tensor_id": f"{graph_id}/tensor/{name}" if name else None} for name in node.input],
            "outputs": [{"name": name, "tensor_id": f"{graph_id}/tensor/{name}" if name else None} for name in node.output],
            "attributes": [attribute_json(attr, graph_id, node_id, add_subgraph, onnx, source_dir, external_files) for attr in node.attribute],
        }
        if node.metadata_props:
            node_result["metadata_props"] = {prop.key: prop.value for prop in node.metadata_props}
        if getattr(node, "device_configurations", None):
            node_result["device_configurations"] = json_safe(list(node.device_configurations))
        if visual_ids is None:
            node_result.pop("visual_id")
        graph_result["nodes"].append(node_result)
        for name in node.input:
            if name:
                consumers.setdefault(name, []).append(node_id)
        for name in node.output:
            if name:
                producers[name] = node_id
    tensor_names = set(producers) | set(consumers)
    tensor_names.update(value.name for value in graph.input)
    tensor_names.update(value.name for value in graph.output)
    tensor_names.update(tensor.name for tensor in graph.initializer)
    tensor_names.update(tensor.values.name for tensor in graph.sparse_initializer)
    tensor_names.update(value.name for value in graph.value_info)
    for name in sorted(tensor_names):
        graph_result["tensors"].append({
            "id": f"{graph_id}/tensor/{name}",
            "name": name,
            "producer": producers.get(name),
            "consumers": consumers.get(name, []),
        })
    graph_result["node_count"] = len(graph_result["nodes"])
    graph_result["tensor_count"] = len(graph_result["tensors"])
    graph_result["subgraphs"] = subgraphs
    return graph_result


def annotate_graph(graph: Any, graph_id: str, global_counter: list[int]) -> None:
    for index, node in enumerate(graph.node):
        global_counter[0] += 1
        visual_id = f"v{global_counter[0]:07d}"
        original = node.name.strip()
        # Netron's "Show Names" view renders node.name in the graph node title.
        node.name = f"[{visual_id}] {node.op_type}" + (f" | {original}" if original else "")
        for attribute in node.attribute:
            if attribute.type == attribute.GRAPH:
                child_id = f"{graph_id}/node/{index:06d}/attr/{attribute.name or 'unnamed'}"
                annotate_graph(attribute.g, child_id, global_counter)
            elif attribute.type == attribute.GRAPHS:
                for sub_index, subgraph in enumerate(attribute.graphs):
                    child_id = f"{graph_id}/node/{index:06d}/attr/{attribute.name or 'unnamed'}/{sub_index:04d}"
                    annotate_graph(subgraph, child_id, global_counter)


def make_visual_copy(model: Any, source: Path) -> Path:
    import onnx
    visual = copy.deepcopy(model)
    counter = [0]
    annotate_graph(visual.graph, "main", counter)
    for function_index, function in enumerate(visual.functions):
        annotate_graph(function, f"function/{function.domain}/{function.name}/{function_index:04d}", counter)
    # Keep the temporary model beside the original so ONNX external-data relative paths resolve unchanged.
    fd, name = tempfile.mkstemp(prefix=".netron-visual-", suffix=source.suffix or ".onnx", dir=source.parent)
    os.close(fd)
    path = Path(name)
    try:
        onnx.save_model(visual, str(path))
    except Exception:
        path.unlink(missing_ok=True)
        raise
    return path


def export_with_netron(model_path: Path, output_dir: Path, formats: list[str], timeout: int, first_node_op: str, first_visual_id: str) -> dict[str, str]:
    try:
        import netron
    except ImportError as error:
        raise RuntimeError("Netron is required. Install it with: pip install netron") from error
    try:
        from playwright.sync_api import sync_playwright
    except ImportError as error:
        raise RuntimeError("Automated image export needs Playwright. Install it with: pip install playwright, then run: playwright install chromium") from error

    address = netron.serve(str(model_path), address=("127.0.0.1", 0), browse=False)
    base_url = f"http://127.0.0.1:{address[1]}/"
    hashes: dict[str, str] = {}
    try:
        cli = shutil.which("playwright-cli")
        if not cli:
            raise RuntimeError("playwright-cli was not found on PATH. Install/configure it before automatic Netron image export.")
        session = f"onnx-netron-{os.getpid()}"
        session_arg = f"-s={session}"
        js_lines = [
            "async (page) => {",
            f"  const timeout = {int(timeout * 1000)};",
            f"  await page.getByText({json.dumps(first_node_op)}, {{ exact: false }}).first().waitFor({{ state: 'visible', timeout }});",
            "  await page.keyboard.press('Control+u');",
            f"  await page.getByText({json.dumps('[' + first_visual_id + ']')}, {{ exact: false }}).first().waitFor({{ state: 'visible', timeout }});",
        ]
        for extension in formats:
            shortcut = "Control+Alt+e" if extension == "svg" else "Control+Shift+e"
            save_path = json.dumps(str((output_dir / f"model.{extension}").resolve()))
            js_lines.extend([
                "  {",
                "    const downloadPromise = page.waitForEvent('download', { timeout });",
                f"    await page.keyboard.press('{shortcut}');",
                "    const download = await downloadPromise;",
                f"    await download.saveAs({save_path});",
                "  }",
            ])
        js_lines.extend(["  return 'Netron export complete';", "}"])
        with tempfile.TemporaryDirectory(prefix="onnx-netron-playwright-") as cli_temp:
            cli_dir = Path(cli_temp)
            code_path = cli_dir / "export.js"
            code_path.write_text("\n".join(js_lines), encoding="utf-8")
            try:
                open_command = [cli, session_arg, "open", base_url]
                chrome_candidates = [shutil.which("chrome"), r"C:\Program Files\Google\Chrome\Application\chrome.exe", r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe"]
                edge_candidates = [shutil.which("msedge"), r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe", r"C:\Program Files\Microsoft\Edge\Application\msedge.exe"]
                if any(candidate and Path(candidate).is_file() for candidate in chrome_candidates):
                    open_command.extend(["--browser", "chrome"])
                elif any(candidate and Path(candidate).is_file() for candidate in edge_candidates):
                    open_command.extend(["--browser", "msedge"])
                opened = subprocess.run(open_command, capture_output=True, text=True, timeout=timeout, check=False, cwd=cli_dir)
                if opened.returncode != 0:
                    raise RuntimeError(f"playwright-cli could not open Netron: {opened.stderr or opened.stdout}")
                run = subprocess.run([cli, session_arg, "run-code", "--filename", str(code_path)], capture_output=True, text=True, timeout=timeout * max(1, len(formats)) + 30, check=False, cwd=cli_dir)
                if run.returncode != 0:
                    raise RuntimeError(f"playwright-cli Netron export failed: {run.stderr or run.stdout}")
                for extension in formats:
                    target = output_dir / f"model.{extension}"
                    if not target.is_file():
                        raise RuntimeError(f"Netron export did not create {target.name}: {run.stdout}")
                    hashes[target.name] = sha256_file(target)
            finally:
                subprocess.run([cli, session_arg, "close"], capture_output=True, text=True, timeout=20, check=False, cwd=cli_dir)
    finally:
        netron.stop(address)
    return hashes


def main() -> int:
    parser = argparse.ArgumentParser(description="Create a paired Netron image and ONNX graph JSON bundle.")
    parser.add_argument("model", type=Path, help="input ONNX model")
    parser.add_argument("--out", type=Path, default=None, help="output directory (default: <model>.inspect)")
    parser.add_argument("--format", choices=("svg", "png", "both"), default="svg", help="Netron image format")
    parser.add_argument("--timeout", type=int, default=120, help="Netron browser load/export timeout in seconds")
    parser.add_argument("--json-only", action="store_true", help="write structured JSON without launching Netron")
    args = parser.parse_args()

    source = args.model.resolve()
    if not source.is_file():
        parser.error(f"ONNX model does not exist: {source}")
    output_dir = (args.out or source.with_name(source.stem + ".inspect")).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)

    try:
        import onnx
    except ImportError:
        print("ONNX Python package is required. Install it with: pip install onnx", file=sys.stderr)
        return 2

    try:
        original = onnx.load_model(str(source), load_external_data=False)
        # Pass a path so ONNX resolves external tensor data relative to the model file.
        onnx.checker.check_model(str(source), full_check=False)
        model = original
        inference: dict[str, Any] = {"status": "not_run"}
        try:
            model = onnx.shape_inference.infer_shapes(original, strict_mode=False)
            inference = {"status": "success"}
        except Exception as error:  # Shape inference is advisory; raw ONNX annotations remain available.
            inference = {"status": "partial_or_failed", "error": f"{type(error).__name__}: {error}"}
        external_files: dict[str, dict[str, Any]] = {}
        visual_ids: dict[str, str] = {}
        manifest = {
            "schema_version": SCHEMA_VERSION,
            "source_model": {
                "file_name": source.name,
                "sha256": sha256_file(source),
                "size_bytes": source.stat().st_size,
                "external_data_files": [],
            },
            "onnx": {
                "ir_version": model.ir_version,
                "opset_imports": [{"domain": item.domain or "ai.onnx", "version": item.version} for item in model.opset_import],
                "producer_name": model.producer_name,
                "producer_version": model.producer_version,
                "domain": model.domain,
                "model_version": model.model_version,
                "doc_string": model.doc_string,
                "metadata_props": {item.key: item.value for item in model.metadata_props},
                "configuration": json_safe(list(getattr(model, "configuration", []))),
                "shape_inference": inference,
            },
            "graphs": [],
            "functions": [],
            "training_info": [],
            "visual_mapping": {
                "method": "visual-only ONNX copy: node names are prefixed with unique IDs and Netron Show Names is enabled",
                "nodes": visual_ids,
            },
            "artifacts": {},
        }
        counter = [0]
        manifest["graphs"].append(build_graph(model.graph, "main", onnx, source.parent, external_files, visual_ids, counter))
        for function_index, function in enumerate(model.functions):
            function_id = f"function/{function.domain or 'ai.onnx'}/{function.name}/{function_index:04d}"
            function_graph = type("GraphAdapter", (), {})()
            function_graph.name = function_id
            function_graph.input = []
            function_graph.output = []
            function_graph.value_info = []
            function_graph.initializer = []
            function_graph.sparse_initializer = []
            function_graph.node = function.node
            result = build_graph(function_graph, function_id, onnx, source.parent, external_files, visual_ids, counter)
            result["function"] = {"domain": function.domain, "name": function.name, "overload": getattr(function, "overload", ""), "inputs": list(function.input), "outputs": list(function.output), "attribute_names": list(function.attribute)}
            result["function"]["attribute_defaults"] = [json_safe(item) for item in getattr(function, "attribute_proto", [])]
            result["function"]["metadata_props"] = {item.key: item.value for item in getattr(function, "metadata_props", [])}
            result["opset_imports"] = [{"domain": item.domain or "ai.onnx", "version": item.version} for item in function.opset_import]
            manifest["functions"].append(result)
        for index, info in enumerate(model.training_info):
            training = {"id": f"training/{index:04d}", "initialization_binding": [{"key": b.key, "value": b.value} for b in info.initialization_binding], "update_binding": [{"key": b.key, "value": b.value} for b in info.update_binding], "visualized_in_overview": False}
            if info.HasField("initialization"):
                training["initialization_graph"] = build_graph(info.initialization, f"{training['id']}/initialization", onnx, source.parent, external_files, None, counter)
            if info.HasField("algorithm"):
                training["algorithm_graph"] = build_graph(info.algorithm, f"{training['id']}/algorithm", onnx, source.parent, external_files, None, counter)
            manifest["training_info"].append(training)
        manifest["source_model"]["external_data_files"] = sorted(external_files.values(), key=lambda value: value.get("path", ""))
        manifest_path = output_dir / "model.graph.json"
        manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

        if not args.json_only:
            formats = ["svg", "png"] if args.format == "both" else [args.format]
            first_node = next((node for graph in manifest["graphs"] for node in graph["nodes"]), None)
            if first_node is None:
                raise RuntimeError("The model graph has no nodes to visualize")
            visual_path = make_visual_copy(model, source)
            try:
                artifact_hashes = export_with_netron(visual_path, output_dir, formats, args.timeout, first_node["op_type"], first_node["visual_id"])
            finally:
                visual_path.unlink(missing_ok=True)
            manifest["artifacts"] = artifact_hashes
            svg_path = output_dir / "model.svg"
            if svg_path.is_file():
                svg_text = svg_path.read_text(encoding="utf-8", errors="replace")
                manifest["visual_mapping"]["svg_overview_nodes"] = [node_id for node_id, visual_id in visual_ids.items() if visual_id in svg_text]
            manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

        print(f"Created: {manifest_path}")
        for name in manifest["artifacts"]:
            print(f"Created: {output_dir / name}")
        def count_graph_nodes(graph: dict[str, Any]) -> int:
            return len(graph.get("nodes", [])) + sum(count_graph_nodes(child) for child in graph.get("subgraphs", []))
        total_nodes = sum(count_graph_nodes(graph) for graph in manifest["graphs"] + manifest["functions"])
        total_nodes += sum(count_graph_nodes(training[key]) for training in manifest["training_info"] for key in ("initialization_graph", "algorithm_graph") if key in training)
        print(f"Nodes: {total_nodes}")
        print(f"Source SHA-256: {manifest['source_model']['sha256']}")
        return 0
    except Exception as error:
        print(f"Export failed: {type(error).__name__}: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
