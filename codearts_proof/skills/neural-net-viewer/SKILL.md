---
name: neural-net-viewer
description: Create a Netron visualization and a matching structured description for the same ONNX model, with verifiable node and tensor cross-references.
---

# Neural Network Viewer

Use this skill when a user wants to inspect a neural network visually and pair the diagram with machine-readable structure. ONNX is the primary structured format. Use Netron for the visualization and ONNX protobuf or the ONNX Python API for the structural manifest; an image alone is never a complete graph description.

## Deliverables

Create a Netron-rendered image and a structured JSON manifest for the same source model. The default bundle is:

```text
<model>.inspect/
├── model.graph.json
└── model.svg
```

Export `model.png` as well when requested. Do not create a separate Markdown report unless the user asks for one.

Record the source ONNX file name, SHA-256, size, IR version, opset imports, and hashes for referenced external tensor-data files. Describe graph inputs and outputs, node IDs and indices, names, domains, operator types, overloads, attributes, input/output tensor references, producer/consumer edges, available shapes and data types, initializers, local functions, nested graphs, and training graphs. Never embed large weight arrays in JSON; record their types, shapes, references, and hashes where available.

## Pairing the diagram and manifest

- Parse the original ONNX model to create the manifest. Do not optimize, prune, fold, or otherwise change its computation and then claim the results describe the original model.
- Assign deterministic IDs based on graph path and node order, such as `main/node/000123`. Keep original names as descriptive fields; names can be empty or duplicated.
- Use the export script to create a temporary visualization-only ONNX copy whose node names are prefixed with unique visual IDs. The original model must remain unchanged. The script enables Netron's Show Names view so IDs appear in the exported image and map through `visual_mapping.nodes` in the JSON.
- Record hashes for the image artifacts and source model. An artifact hash identifies an output file; it does not establish which model produced it.
- Netron's overview may not expand control-flow or nested subgraphs. If the manifest includes nodes not visible in the overview image, report that limitation and do not claim that the image individually displays every node.
- Do not claim that the Netron Python package exposes a stable direct image-export API. The bundled script uses the Netron web viewer and Playwright CLI keyboard shortcuts. If this workflow fails, explain the failure and provide the manual Netron export steps: PNG uses `Ctrl+Shift+E`, SVG uses `Ctrl+Alt+E`.

## Required environment

Before automatic SVG or PNG export, confirm:

- Python 3.10 or later, with `onnx` and `netron` installed: `python -m pip install onnx netron`.
- `playwright-cli` is installed and available on `PATH`. Reuse it when already present; do not install the Python Playwright package for this workflow.
- Google Chrome or Microsoft Edge is installed. If neither is available, install the Playwright Chromium browser with `playwright-cli install-browser chromium` and confirm the CLI can launch its default Chromium.
- ONNX external-data files remain at the relative paths recorded in the model.

Check the environment with:

```powershell
python -c "import onnx, netron; print(onnx.__version__, netron.__version__)"
playwright-cli --version
```

The export script starts a local Netron server, creates a temporary Playwright CLI session, exports through Netron's own viewer, and closes the session and server. It requires no project dependency changes.

## Workflow

1. Identify the input ONNX model and requested image format. Do not download a model or change project dependencies unless asked.
2. Use `scripts/export_onnx_bundle.py` to create the image and JSON from the same model:

   ```powershell
   python "<skill-path>/scripts/export_onnx_bundle.py" model.onnx --format both
   ```

   Supported formats are `svg`, `png`, and `both`. By default, output goes to `<model>.inspect/`. Use `--json-only` when the visualization environment is unavailable; clearly state that no image was exported.
3. The exporter checks the ONNX model, attempts shape inference, and marks inference failure or unknown dimensions instead of guessing. It does not modify the source model. If a field cannot be determined from the model, preserve it as unknown or unavailable.
4. Verify the bundle and, when possible, the supplied source model:

   ```powershell
   python "<skill-path>/scripts/verify_onnx_bundle.py" "model.inspect/model.graph.json" --model model.onnx
   ```

   The verifier checks source and external-data hashes, artifact hashes, and graph cross-references. It warns when nodes mapped in the JSON are absent from the overview SVG, which can happen with collapsed nested graphs.
5. Report the bundle paths, source-model identity, pairing method, verification results, and any visibility or inference limitations.

For Netron installation and supported model formats, see the [Netron README](https://github.com/lutzroeder/netron#install).
