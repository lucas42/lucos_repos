# C4 model of the lucOS estate

**The generated model has moved.** Per [ADR-0008](../adr/0008-c4-output-relocation.md), the `lucos_repos` sweep now writes `model.dsl`, `landscape.md` and `divergences.md` to the dedicated output repo **[`lucas42/lucos_architecture_models`](https://github.com/lucas42/lucos_architecture_models)**, authenticated as the scoped `lucos-architecture-writer` App — not here. Generation logic (design and rationale: [ADR-0006](../adr/0006-c4-estate-model.md)) stays in this repo.

The `model.dsl`/`landscape.md`/`divergences.md` files still committed in this directory are a **stale leftover** from before the retarget ([#446](https://github.com/lucas42/lucos_repos/issues/446)) and will be deleted once the first cross-repo sweep succeeds in production. `prototype-generator.py` (the executable spec the Go generator in `../../src/c4.go` was ported from) stays here permanently.
