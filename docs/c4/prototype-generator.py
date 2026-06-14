# PROTOTYPE C4 generator for the lucOS estate (lucos_repos ADR-0006).
# This is the executable spec for the Go port into the sweep (ADR-0006 follow-up 1),
# NOT the production implementation. Run from a checkout with the sibling lucos repos
# present; probes public /_info live. Output: model.dsl, landscape.md, divergences.md.
#
"""
Prototype C4-model generator for the lucOS estate.

Reads tangible data sources and emits:
  - model.dsl       Structurizr DSL (model of record: landscape + deployment views)
  - landscape.md    Mermaid flowchart of the connected core (GitHub-native viewing)
  - divergences.md  audit findings (drift between sources)

Edge types, by source:
  sync  (solid)  : /_info  -> checks[].dependsOn        (live, self-declared)
  async (dotted) : loganne webhooks-config.json         (static config)
  Deployment placement: configy systems.yaml `hosts:`   (static, canonical)
Trust edges (creds linkedCredentials) are deferred to phase 2 (needs a creds read cred).
"""
import json, subprocess, sys, os
import yaml

ROOT = os.path.expanduser("~/sandboxes")
OUT = "/tmp/c4gen"
SYSTEMS = yaml.safe_load(open(f"{ROOT}/lucos_configy/config/systems.yaml"))
HOSTS = yaml.safe_load(open(f"{ROOT}/lucos_configy/config/hosts.yaml"))
LOGANNE = json.load(open(f"{ROOT}/lucos_loganne/src/webhooks-config.json"))

# --- build domain -> system map (canonical, from configy) ---
domain2sys = {}
for sys_name, v in SYSTEMS.items():
    if v and v.get("domain"):
        domain2sys[v["domain"]] = sys_name

divergences = []

# --- 1. sync edges: probe /_info dependsOn ---
sync_edges = set()          # (from_sys, to_sys)
info_system_name = {}       # configy key -> /_info reported system
unreachable = []
for sys_name, v in SYSTEMS.items():
    if not v or not v.get("domain"):
        continue
    dom = v["domain"]
    try:
        body = subprocess.run(
            ["curl", "-s", "--max-time", "5", f"https://{dom}/_info"],
            capture_output=True, text=True, timeout=8).stdout
        info = json.loads(body)
    except Exception:
        unreachable.append(sys_name)
        continue
    reported = info.get("system")
    if reported:
        info_system_name[sys_name] = reported
        if reported != sys_name:
            divergences.append(f"- `{sys_name}` (configy) reports `system: {reported}` in /_info on {dom}")
    for chk in (info.get("checks") or {}).values():
        dep = chk.get("dependsOn")
        if dep:
            deps = dep if isinstance(dep, list) else [dep]
            for d in deps:
                sync_edges.add((sys_name, d))

# --- 2. async edges: loganne event fan-out ---
# producer is whoever posts the event; loganne is the broker. We model broker fan-out:
# event consumers are the webhook targets. Edge: loganne --event--> consumer-system.
async_edges = set()         # (consumer_sys, event)  consumed-from-loganne
event_consumers = {}        # event -> [systems]
for key, val in LOGANNE.items():
    if key == "consumerTokens" or not isinstance(val, list):
        continue
    event = key
    consumers = []
    for url in val:
        # url like https://arachne.l42.eu/webhook
        dom = url.split("/")[2]
        sysn = domain2sys.get(dom)
        if not sysn:
            divergences.append(f"- loganne event `{event}` -> `{dom}` has no matching configy system")
            continue
        consumers.append(sysn)
        async_edges.add((sysn, event))
    event_consumers[event] = consumers

# --- emit Structurizr DSL ---
def ident(s):  # structurizr identifiers: no dots/dashes
    return s.replace("-", "_").replace(".", "_")

dsl = []
dsl.append("workspace \"lucOS estate\" \"Generated C4 model — DO NOT EDIT BY HAND\" {")
dsl.append("    model {")
dsl.append("        lucas = person \"lucas42\"")
for sys_name, v in SYSTEMS.items():
    v = v or {}
    desc = v.get("domain", "(no public domain)")
    dsl.append(f'        {ident(sys_name)} = softwareSystem "{sys_name}" "{desc}"')
dsl.append("")
dsl.append("        # sync dependencies (/_info dependsOn)")
for a, b in sorted(sync_edges):
    if b in SYSTEMS:
        dsl.append(f'        {ident(a)} -> {ident(b)} "depends on (sync)"')
dsl.append("")
dsl.append("        # async event subscriptions (loganne)")
for sysn, event in sorted(async_edges):
    dsl.append(f'        lucos_loganne -> {ident(sysn)} "{event}"')
dsl.append("    }")
dsl.append("    views {")
dsl.append("        systemLandscape \"estate\" { include * autolayout lr }")
dsl.append("        theme default")
dsl.append("    }")
dsl.append("}")
os.makedirs(OUT, exist_ok=True)
open(f"{OUT}/model.dsl", "w").write("\n".join(dsl) + "\n")

# --- emit Mermaid (connected core only, to stay readable) ---
connected = set()
for a, b in sync_edges:
    connected.add(a); connected.add(b)
for sysn, _ in async_edges:
    connected.add(sysn); connected.add("lucos_loganne")

mer = ["# lucOS estate — connected core (generated)", "",
       "```mermaid", "flowchart LR"]
for s in sorted(connected):
    mer.append(f'  {ident(s)}["{s}"]')
mer.append("  %% sync deps (solid)")
for a, b in sorted(sync_edges):
    if b in SYSTEMS:
        mer.append(f"  {ident(a)} --> {ident(b)}")
mer.append("  %% async events (dotted, via loganne)")
for sysn, event in sorted(async_edges):
    mer.append(f"  lucos_loganne -.{event}.-> {ident(sysn)}")
mer.append("```")
open(f"{OUT}/landscape.md", "w").write("\n".join(mer) + "\n")

# --- emit divergences ---
open(f"{OUT}/divergences.md", "w").write(
    "# Source divergences (audit findings)\n\n" +
    ("\n".join(divergences) if divergences else "None.") +
    f"\n\nUnreachable /_info: {', '.join(unreachable) or 'none'}\n")

print(f"systems: {len(SYSTEMS)}  sync-edges: {len(sync_edges)}  "
      f"async-edges: {len(async_edges)}  divergences: {len(divergences)}  "
      f"unreachable: {len(unreachable)}")
