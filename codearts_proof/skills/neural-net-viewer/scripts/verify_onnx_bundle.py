#!/usr/bin/env python3
"""Verify hashes and cross-references in an ONNX/Netron export bundle."""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path
from typing import Any


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def walk_graph(graph: dict[str, Any]):
    yield graph
    for subgraph in graph.get("subgraphs", []):
        yield from walk_graph(subgraph)


def main() -> int:
    parser = argparse.ArgumentParser(description="Check model identity, image hashes, and node/tensor IDs in a bundle.")
    parser.add_argument("manifest", type=Path, help="path to model.graph.json")
    parser.add_argument("--model", type=Path, help="original ONNX file; verify it matches the recorded SHA-256")
    args = parser.parse_args()

    manifest_path = args.manifest.resolve()
    if not manifest_path.is_file():
        parser.error(f"manifest not found: {manifest_path}")
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except Exception as error:
        print(f"FAIL: cannot read JSON: {error}", file=sys.stderr)
        return 1

    errors: list[str] = []
    warnings: list[str] = []
    base = manifest_path.parent
    expected_model_hash = manifest.get("source_model", {}).get("sha256")
    if not expected_model_hash:
        errors.append("source_model.sha256 is missing")
    if args.model:
        model_path = args.model.resolve()
        if not model_path.is_file():
            errors.append(f"model not found: {model_path}")
        elif sha256_file(model_path) != expected_model_hash:
            errors.append("the supplied ONNX file does not match source_model.sha256")
        else:
            print(f"OK: ONNX hash matches {model_path}")
            for external in manifest.get("source_model", {}).get("external_data_files", []):
                relative = external.get("path")
                expected_external_hash = external.get("sha256")
                if not relative or not expected_external_hash:
                    continue
                external_path = (model_path.parent / relative).resolve()
                if not external_path.is_file():
                    errors.append(f"external data file missing: {external_path}")
                elif sha256_file(external_path) != expected_external_hash:
                    errors.append(f"external data hash mismatch: {external_path}")
                else:
                    print(f"OK: external data hash matches {external_path}")

    node_ids: set[str] = set()
    node_types: dict[str, str] = {}
    visual_node_ids: set[str] = set()
    visual_ids: set[str] = set()
    tensor_ids: set[str] = set()
    graph_list = []
    for root in manifest.get("graphs", []) + manifest.get("functions", []):
        graph_list.extend(walk_graph(root))
    for training in manifest.get("training_info", []):
        for key in ("initialization_graph", "algorithm_graph"):
            if training.get(key):
                graph_list.extend(walk_graph(training[key]))
    for graph in graph_list:
        for node in graph.get("nodes", []):
            node_id = node.get("id")
            visual_id = node.get("visual_id")
            if not node_id:
                errors.append(f"node without id in graph {graph.get('id')}")
            elif node_id in node_ids:
                errors.append(f"duplicate node id: {node_id}")
            else:
                node_ids.add(node_id)
                node_types[node_id] = node.get("op_type", "unknown")
            if not visual_id:
                if graph.get("visualized_in_overview") is not False and not graph.get("id", "").startswith("training/"):
                    errors.append(f"node {node_id} has no visual_id")
            elif visual_id in visual_ids:
                errors.append(f"duplicate visual_id: {visual_id}")
            else:
                visual_ids.add(visual_id)
                visual_node_ids.add(node_id)
        for tensor in graph.get("tensors", []):
            tensor_id = tensor.get("id")
            if tensor_id in tensor_ids:
                errors.append(f"duplicate tensor id: {tensor_id}")
            elif tensor_id:
                tensor_ids.add(tensor_id)
            producer = tensor.get("producer")
            if producer and producer not in node_ids and not any(n.get("id") == producer for g in graph_list for n in g.get("nodes", [])):
                errors.append(f"tensor {tensor_id} references unknown producer {producer}")
            for consumer in tensor.get("consumers", []):
                if not any(n.get("id") == consumer for g in graph_list for n in g.get("nodes", [])):
                    errors.append(f"tensor {tensor_id} references unknown consumer {consumer}")

    expected_mapping = manifest.get("visual_mapping", {}).get("nodes", {})
    if len(expected_mapping) != len(visual_node_ids):
        errors.append(f"visual_mapping has {len(expected_mapping)} nodes but {len(visual_node_ids)} nodes are marked visualized")
    elif set(expected_mapping) != visual_node_ids:
        errors.append("visual_mapping node IDs do not match the visualized graph node IDs")
    else:
        node_visual_lookup = {node["id"]: node.get("visual_id") for graph in graph_list for node in graph.get("nodes", []) if node.get("id")}
        for node_id, visual_id in expected_mapping.items():
            if node_visual_lookup.get(node_id) != visual_id:
                errors.append(f"visual_mapping for {node_id} does not match its node visual_id")

    artifacts = manifest.get("artifacts", {})
    if not artifacts:
        warnings.append("no visual artifact hashes recorded; this may be a --json-only bundle")
    for filename, expected_hash in artifacts.items():
        artifact = (base / filename).resolve()
        if not artifact.is_file():
            errors.append(f"artifact missing: {artifact}")
        elif sha256_file(artifact) != expected_hash:
            errors.append(f"artifact hash mismatch: {artifact}")
        else:
            print(f"OK: {filename} hash matches")
            if filename.lower().endswith(".svg") and "svg_overview_nodes" in manifest.get("visual_mapping", {}):
                svg_text = artifact.read_text(encoding="utf-8", errors="replace")
                overview_ids = set(manifest["visual_mapping"]["svg_overview_nodes"])
                for node_id in overview_ids:
                    visual_id = expected_mapping.get(node_id)
                    if not visual_id or visual_id not in svg_text:
                        errors.append(f"SVG overview lists {node_id} but its visual ID is missing")
                not_in_overview = set(expected_mapping) - overview_ids
                if not_in_overview:
                    hidden_types = {node_types.get(node_id, "unknown") for node_id in not_in_overview}
                    if hidden_types == {"Constant"}:
                        warnings.append(
                            f"{len(not_in_overview)} Constant node(s) are not separate boxes in the overview SVG; "
                            "Netron represents eligible Constant outputs as initializer tensors. Their full node records remain in the JSON manifest."
                        )
                    else:
                        type_summary = ", ".join(sorted(hidden_types))
                        warnings.append(
                            f"{len(not_in_overview)} mapped node(s) ({type_summary}) are not visible in this overview SVG. "
                            "Netron may omit or collapse these nodes in its overview; their records remain in the JSON manifest."
                        )

    if errors:
        for error in errors:
            print(f"FAIL: {error}", file=sys.stderr)
        for warning in warnings:
            print(f"WARN: {warning}")
        return 1
    for warning in warnings:
        print(f"WARN: {warning}")
    print(f"OK: {len(node_ids)} nodes, {len(tensor_ids)} tensors, {len(artifacts)} visual artifacts; node/tensor references are consistent.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
