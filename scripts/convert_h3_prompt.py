#!/usr/bin/env python3
# Convert ComfyUI UI-format workflow (with one subgraph node) to API prompt JSON.
# Usage: convert_h3_prompt.py <workflow.json> <out.json> --set sg_input=json_value ...
import json, sys

wf_path, out_path = sys.argv[1], sys.argv[2]
overrides = {}
for a in sys.argv[3:]:
    if a.startswith("--set="):
        k, v = a[6:].split("=", 1)
        overrides[k] = json.loads(v)

wf = json.load(open(wf_path))
sg = wf["definitions"]["subgraphs"][0]
outer_links = {l[0]: l for l in wf.get("links", [])}
sg_links = {l["id"]: l for l in sg["links"]}
sg_node = next(n for n in wf["nodes"] if n["type"] == sg.get("id"))

# sg-input widget values: all sg inputs are widgets except image sockets
NON_WIDGET = {"first_frame", "last_frame"}
wvals = sg_node["widgets_values"]
wi = 0
sg_widget = {}
for inp in sg["inputs"]:
    if inp["name"] in NON_WIDGET:
        continue
    sg_widget[inp["name"]] = wvals[wi]
    wi += 1
sg_link = {}
for inp in sg_node["inputs"]:
    if inp.get("link") is not None:
        l = outer_links[inp["link"]]
        sg_link[inp["name"]] = [str(l[1]), l[2]]

def sg_value(name):
    if name in sg_link:
        return sg_link[name]
    if name in overrides:
        return overrides[name]
    return sg_widget.get(name)

# widget sequences per class_type ("__c__" = UI control flag, consume & drop)
WSEQ = {
    "VAELoader": ["vae_name"],
    "VAEDecode": [], "VAEDecodeAudio": [], "SamplerCustomAdvanced": [], "BasicGuider": [],
    "KSamplerSelect": ["sampler_name"],
    "BasicScheduler": ["scheduler", "steps", "denoise"],
    "UNETLoader": ["unet_name", "weight_dtype"],
    "CLIPLoader": ["clip_name", "type", "device"],
    "RandomNoise": ["noise_seed", "__c__"],
    "CreateVideo": ["fps", "bit_depth", "color_space"],
    "MiniMaxH3ImageToVideo": ["prompt"],
    "ComfyMathExpression": ["expression"],
    "PrimitiveFloat": ["value"], "PrimitiveInt": ["value", "__c__"], "PrimitiveBoolean": ["value"],
    "LoraLoaderModelOnly": ["lora_name", "strength_model"],
    "ComfySwitchNode": ["switch"],
    "SaveVideo": ["filename_prefix", "format", "codec"],
    "ResolutionSelector": ["aspect_ratio", "megapixels", "multiple"],
}

prompt = {}

def emit(node, link_lookup, inner):
    cid = str(node["id"])
    inputs = {}
    linked = {}
    for inp in node.get("inputs", []):
        if inp.get("link") is None:
            continue
        l = link_lookup[inp["link"]]
        if inner and l["origin_id"] == -10:
            val = sg_value(sg["inputs"][l["origin_slot"]]["name"])
            if val is None:
                continue
            inputs[inp["name"]] = val
        else:
            inputs[inp["name"]] = [str(l["origin_id"]), l["origin_slot"]]
        linked[inp["name"]] = True
    wq = list(node.get("widgets_values") or [])
    for i, name in enumerate(WSEQ.get(node["type"], [])):
        if i >= len(wq):
            break
        if name == "__c__" or name in linked:
            continue
        inputs[name] = wq[i]
    prompt[cid] = {"class_type": node["type"], "inputs": inputs}

for n in sg["nodes"]:
    emit(n, sg_links, True)
for n in wf["nodes"]:
    if n["id"] == sg_node["id"] or n["type"] in ("MarkdownNote", "Note"):
        continue
    emit(n, {l[0]: {"origin_id": l[1], "origin_slot": l[2]} for l in wf.get("links", [])}, False)

# remap links that referenced the subgraph node output -> inner producing node
sg_out = {}
for oi, o in enumerate(sg["outputs"]):
    lids = o.get("linkIds") or o.get("links") or []
    if lids:
        l = sg_links[lids[0]]
        sg_out[oi] = [str(l["origin_id"]), l["origin_slot"]]
for entry in prompt.values():
    for k, v in list(entry["inputs"].items()):
        if isinstance(v, list) and v[0] == str(sg_node["id"]):
            entry["inputs"][k] = sg_out[v[1]]

json.dump(prompt, open(out_path, "w"), ensure_ascii=False, indent=1)
print("nodes:", len(prompt))
for k, v in prompt.items():
    print(" ", k, v["class_type"], json.dumps(v["inputs"], ensure_ascii=False)[:160])
