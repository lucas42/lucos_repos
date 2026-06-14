# C4 model of the lucOS estate

A C4 architecture model **generated from tangible data sources** (configy, `/_info`, loganne config, docker-compose, creds), not hand-maintained. Design and rationale: [ADR-0006](../adr/0006-c4-estate-model.md).

| File | What it is |
|---|---|
| `model.dsl` | Structurizr DSL — the **model of record** (all systems; landscape + deployment views). |
| `landscape.md` | Mermaid view of the connected core, viewable inline in GitHub. |
| `divergences.md` | Audit findings — drift between sources (routed through audit-finding issues, ADR-0004). |
| `prototype-generator.py` | Working prototype; the executable spec for the Go port into the `lucos_repos` sweep (ADR-0006 follow-up 1). |

**The committed files are a dated first-cut.** Automated regeneration in the CI sweep is [#422](https://github.com/lucas42/lucos_repos/issues/422) / ADR-0006 follow-up 1; until that lands, treat the artifact as a snapshot, and `git log` of this directory as the architecture changelog from this point on.
