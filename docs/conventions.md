# Convention catalogue

**This file is generated — do not edit by hand.** It is rendered from the `Convention` definitions in `conventions/*.go`, which are the authoritative, enforced source of truth.

Regenerate with `go run ./src conventions > docs/conventions.md`. `TestConventionCatalogueIsCurrent` fails the build if this file drifts from the source.

Documentation elsewhere (e.g. the `lucos_claude_config` reference docs) should **link to this catalogue for enforced rules rather than paraphrasing them** — see lucos_repos ADR-0007 for why.

There are **28** registered conventions.

---

## `allow-auto-merge`

System and component repositories must have the "Allow auto-merge" setting enabled

- **Applies to:** system, component

**Why this matters**

Auto-merge allows PRs to be merged automatically once all required status checks pass and any required reviews are approved. Without this setting enabled, agent-opened PRs (e.g. from Dependabot or lucos-code-reviewer) cannot auto-merge — a human must manually merge every PR, which creates a bottleneck and means security updates sit open longer than necessary.

**Suggested fix**

Enable "Allow auto-merge" in the repository's Settings → General page, under the "Pull Requests" section.

---

## `auto-merge-secrets`

Repos with a code-reviewer auto-merge workflow pass LUCOS_CI_APP_ID and LUCOS_CI_PRIVATE_KEY to the reusable workflow and have both configured as Actions secrets on the repo

- **Applies to:** system, component
- **Excluded repos:** lucas42/.github

**Why this matters**

The code-reviewer auto-merge reusable workflow declares LUCOS_CI_APP_ID and LUCOS_CI_PRIVATE_KEY as required secrets (migrated from CODE_REVIEWER_* in v1.15.0). If the caller workflow doesn't pass them, the reusable job fails at startup — auto-merge never runs and there is no obvious error signal.

**Suggested fix**

Ensure the `.github/workflows/code-reviewer-auto-merge.yml` workflow passes both secrets to the reusable workflow:

```yaml
jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/code-reviewer-auto-merge.yml@<commit-sha>
    secrets:
      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}
      LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}
```

You also need to ensure `LUCOS_CI_APP_ID` and `LUCOS_CI_PRIVATE_KEY` are set as Actions secrets on this repository. Ask lucos-system-administrator to set them.

---

## `branch-protection-enabled`

System and component repositories must have branch protection rules enabled on the main branch, without requiring approvals or requiring branches to be up to date

- **Applies to:** system, component

**Why this matters**

Branch protection prevents direct pushes to main and can enforce required status checks before merging. Without it, accidental or malicious direct pushes can bypass CI and deploy untested code. Requiring approvals and requiring branches to be up to date are both disabled because they block Dependabot PRs from auto-merging when more than one is open, causing security updates to pile up.

**Suggested fix**

Enable branch protection on `main` in the repository's Settings → Branches page. At minimum, require pull requests before merging. Ensure "Require approvals" is disabled and "Require branches to be up to date before merging" is disabled — both settings block Dependabot auto-merge. Note: admin bypass is a known and accepted residual risk for this organisation — admins can override protection rules by design.

---

## `circleci-config-exists`

System and component repositories must have a .circleci/config.yml file

- **Applies to:** system, component
- **Excluded repos:** lucas42/.github

**Why this matters**

Without a CircleCI config, changes to this repository are not automatically built, tested, or deployed. This means code changes require manual intervention to reach production, which is error-prone and slows down delivery.

**Suggested fix**

Add a `.circleci/config.yml` following the standard lucos CI template (see the lucos CLAUDE.md for the canonical config).

---

## `circleci-deploy-serial-group`

Every `lucos/build*` job must set `serial-group: << pipeline.project.slug >>/build/<< pipeline.git.branch >>`; every `lucos/deploy-*` job must set `serial-group: deploy-<host>`

- **Applies to:** system, component
- **Excluded repos:** lucas42/lucos_deploy_orb

**Why this matters**

Build serial groups prevent concurrent main-branch pipelines from computing the same VERSION in parallel, which causes Docker Hub images to be overwritten and git tags to drift. The branch-scoped form also prevents PR pipelines from blocking behind main-branch builds. Deploy serial groups prevent concurrent deploys to the same host from racing in containerd (blob-lease conflicts observed 2026-04-21 during an estate-wide rollout).

**Suggested fix**

Add the correct `serial-group` to each job in the `jobs:` list of each workflow in `.circleci/config.yml`:

```yaml
workflows:
  build:
    jobs:
      - lucos/build:
          serial-group: << pipeline.project.slug >>/build/<< pipeline.git.branch >>
      - lucos/deploy-avalon:
          serial-group: deploy-avalon
```

---

## `circleci-has-release-job`

Component CircleCI config must include at least one `lucos/release-*` job

- **Applies to:** component

**Why this matters**

Component repos are shared libraries or infrastructure that other services depend on. The `lucos/release-*` job publishes new versions to the package registry. Without it, updates to the component cannot be consumed by downstream services.

**Suggested fix**

Add a `lucos/release-*` job (e.g. `lucos/release-npm`) to the `jobs:` list in a workflow in `.circleci/config.yml`. Refer to the lucos deploy orb documentation for the correct job name for your package type.

---

## `circleci-jobs-in-required-checks`

CircleCI test* and build* jobs appear in the required status checks for the main branch

- **Applies to:** system, component

**Why this matters**

Without required status checks, auto-merge can complete before CircleCI finishes — meaning a broken build or failing test can land silently on main. Requiring test and build jobs as status checks ensures that code cannot merge until CI has confirmed they pass.

**Suggested fix**

Go to the repository's Settings → Branches → Branch protection rules for `main`. Under 'Require status checks to pass before merging', add each CircleCI test and build job as a required check. The exact check name must match what CircleCI reports in the GitHub Checks tab (e.g. `lucos/build-amd64` for orb jobs, or `test` for simple jobs). Trigger a pull request first to make the check names available in the search box.

---

## `circleci-no-forbidden-jobs`

Non-system, non-component repositories must not include `lucos/release-*` or `lucos/deploy-*` jobs in their CircleCI config

- **Applies to:** all repo types

**Why this matters**

Release and deploy jobs are reserved for components and systems respectively. Including them in other repo types (e.g. scripts) indicates a misconfiguration — either the repo type in lucos_configy is wrong, or the CI config contains jobs that shouldn't be there.

**Suggested fix**

Remove any `lucos/release-*` and `lucos/deploy-*` jobs from the CircleCI config. If this repo should be deploying to a server or releasing a package, update its type in lucos_configy (`config/systems.yaml` or `config/components.yaml`) accordingly.

---

## `circleci-system-deploy-jobs`

System CircleCI config must include exactly the correct `lucos/deploy-*` jobs for its configured hosts

- **Applies to:** system

**Why this matters**

Each host listed in lucos_configy for a system needs its own deploy job so that changes are automatically deployed to every target host. Extra deploy jobs risk deploying to hosts that aren't configured to run the service.

**Suggested fix**

Edit the `jobs:` list in `.circleci/config.yml` to include exactly one `lucos/deploy-{host}` job per host listed in lucos_configy — no more, no fewer. Check `lucos_configy/config/systems.yaml` for the authoritative list of hosts.

---

## `circleci-uses-lucos-orb`

CircleCI config must declare the lucos deploy orb (`lucos: lucos/deploy@0`)

- **Applies to:** system, component
- **Excluded repos:** lucas42/lucos_deploy_orb

**Why this matters**

The lucos deploy orb provides standardised build and deploy jobs. Without it, repos must implement their own build/deploy logic, leading to inconsistency and maintenance burden.

**Suggested fix**

Add the following to the `orbs:` section of `.circleci/config.yml`:

```yaml
orbs:
  lucos: lucos/deploy@0
```

---

## `code-reviewer-auto-merge-workflow`

System and component repos have a code-reviewer auto-merge workflow referencing the shared reusable workflow with minimal permissions

- **Applies to:** system, component
- **Excluded repos:** lucas42/.github

**Why this matters**

The code-reviewer auto-merge workflow ensures approved PRs are merged automatically. The shared reusable workflow checks unsupervisedAgentCode at runtime from configy: repos with it enabled auto-merge on lucos-code-reviewer[bot] approval; others auto-merge on lucas42 approval. Without this workflow, PRs require manual merging. The shared workflow also closes linked issues when a bot-opened PR is merged, which the GITHUB_TOKEN cannot do. The caller must declare `permissions: contents: read` (the minimum required to fetch the reusable workflow definition) — all privileged operations go through the reusable workflow's own GitHub App token.

**Suggested fix**

Add a `.github/workflows/code-reviewer-auto-merge.yml` file that calls the shared reusable workflow:

```yaml
name: Auto-merge on code reviewer approval

on:
  pull_request_review:
    types:
      - submitted
  pull_request:
    types:
      - closed

permissions:
  contents: read

jobs:
  reusable:
    uses: lucas42/.github/.github/workflows/reusable-code-reviewer-auto-merge.yml@<commit-sha>
    secrets:
      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}
      LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}
```

The workflow requires `LUCOS_CI_APP_ID` and `LUCOS_CI_PRIVATE_KEY` as Actions secrets — these are the standard lucos CI credentials and are already present on most repos. Without them the workflow silently fails to generate a GitHub App token and auto-merge never runs. If they are missing, ask lucos-site-reliability or lucos-system-administrator to provision the standard lucos CI credentials.

---

## `codeql-workflow-security-settings`

codeql-analysis.yml has required security settings: pull_request trigger, schedule trigger, top-level permissions, security-events: write on analyze job, and explicit language matrix matching required Analyze checks

- **Applies to:** system, component

**Why this matters**

A CodeQL workflow that only runs on push misses vulnerabilities introduced in PRs. A schedule trigger catches new vulnerabilities in unchanged code. A top-level permissions block restricts the default token scope. And `security-events: write` on the analyze job is required for CodeQL to upload its findings to GitHub. An explicit language matrix ensures CodeQL runs for all required languages on every PR — auto-detected languages may be skipped on PRs that don't touch files in that language, silently blocking merges.

**Suggested fix**

Ensure your `codeql-analysis.yml` includes:

1. A `pull_request:` entry in the `on:` block
2. A `schedule:` entry with a `cron` value in the `on:` block
3. A top-level `permissions:` key in the workflow
4. `security-events: write` in the analyze job's `permissions` block
5. An explicit `strategy.matrix.language` list covering all languages in required `Analyze (X)` status checks

Example:
```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
  schedule:
    - cron: '0 6 * * 1'

permissions: {}

jobs:
  analyze:
    strategy:
      matrix:
        language: [javascript]
    permissions:
      security-events: write
```

---

## `container-naming`

Every container_name in docker-compose.yml uses the lucos_{project}_{role} naming convention

- **Applies to:** system

**Why this matters**

The ecosystem convention for container names is `lucos_{project}_{role}` (e.g. `lucos_photos_api`, `lucos_arachne_web`). Many older services use short names without any prefix (`monitoring`, `root`, `time`), which become ambiguous in `docker ps` output as the number of containers on a single host grows. Consistent naming makes it easy to correlate running containers with their source repo.

**Suggested fix**

Rename the `container_name` in `docker-compose.yml` so it starts with the repo name (e.g. for repo `lucos_monitoring`, use `lucos_monitoring_web` or just `lucos_monitoring` for single-container services).

**Important:** Docker may treat a renamed container as a new one rather than replacing the old one. When deploying a rename, stop the old container before starting the renamed one to avoid port conflicts and orphaned containers.

---

## `delete-branch-on-merge`

System and component repositories must have "Automatically delete head branches" enabled

- **Applies to:** system, component

**Why this matters**

When "Automatically delete head branches" is disabled, merged PR branches accumulate indefinitely. Stale branches clutter the repository, make it harder to navigate open branches, and can lead to confusion about which branches are still active.

**Suggested fix**

Enable "Automatically delete head branches" in the repository's Settings → General page, under the "Pull Requests" section.

---

## `dependabot-auto-merge-workflow`

Repository has a Dependabot auto-merge workflow that references the shared reusable workflow with LUCOS_CI_APP_ID and LUCOS_CI_PRIVATE_KEY configured as both Actions secrets and Dependabot secrets

- **Applies to:** system, component, script
- **Excluded repos:** lucas42/.github

**Why this matters**

Without auto-merge configured, Dependabot PRs pile up and require manual merging. The shared reusable workflow ensures consistent auto-merge behaviour across all repos. Repos that implement their own logic drift from the standard and may miss security fixes applied to the central workflow.

**Suggested fix**

Add a `.github/workflows/dependabot-auto-merge.yml` file that calls the shared reusable workflow:

```yaml
name: Dependabot auto-merge

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  pull-requests: write
  contents: write

jobs:
  dependabot:
    uses: lucas42/.github/.github/workflows/dependabot-auto-merge.yml@<commit-sha>
    secrets:
      LUCOS_CI_APP_ID: ${{ secrets.LUCOS_CI_APP_ID }}
      LUCOS_CI_PRIVATE_KEY: ${{ secrets.LUCOS_CI_PRIVATE_KEY }}
```

Note: use `pull_request` (not `pull_request_target`) and include the top-level `permissions:` block. Using `pull_request_target` with a reusable workflow call causes `startup_failure` on every non-Dependabot PR. Do not use `secrets: inherit`. The `LUCOS_CI_APP_ID` and `LUCOS_CI_PRIVATE_KEY` secrets must be configured in **both** the Actions secret store and the Dependabot secret store (Settings → Security → Secrets and variables). GitHub only exposes Dependabot secrets — not Actions secrets — when a Dependabot PR triggers the workflow. Without them in the Dependabot store, the reusable workflow falls back to GITHUB_TOKEN, which suppresses push events and breaks CodeQL required status checks.

---

## `dependabot-configured`

Repository has a valid .github/dependabot.yml with github-actions monitoring and allow-all on all entries

- **Applies to:** all repo types

**Why this matters**

Any repo without Dependabot configured is flying blind on dependency vulnerabilities. Supply chain attacks via GitHub Actions are a growing attack class, so keeping action versions up to date is critical. Allowing all dependency types keeps deps current so that when critical security patches land, they arrive on a well-maintained base rather than months of accumulated drift. Grouping updates by type collapses the daily wave of individual Dependabot PRs into ~2 PRs per ecosystem, which reduces deploy-wave noise, CI concurrency saturation, and monitoring alert churn.

**Suggested fix**

Create or update `.github/dependabot.yml` to include:

1. At least one entry with `package-ecosystem: github-actions` and `directory: /`
2. An `allow` block with `dependency-type: all` on every update entry
3. A `groups` block on every update entry covering `minor`, `patch`, and `major`

Example:
```yaml
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
  - package-ecosystem: npm
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
```

---

## `docker-dependabot-updater-present`

Every repo that builds Docker images has a docker Dependabot updater entry for each built-service Dockerfile directory

- **Applies to:** all repo types

**Why this matters**

A missing docker Dependabot entry means the repo is completely unmonitored for base-image CVEs. A FROM python:/golang:/node:/etc. line pulls in an OS package set and a language runtime, both of which accrue CVEs over time. Without a docker updater entry, those vulnerabilities are never surfaced to the team — even though the same rationale that mandates github-actions monitoring (supply chain attacks, dependency drift) applies at least as strongly to base images. Non-Python base images (Go, Node, Rust, Java, etc.) are just as affected; the absence of a docker entry is silently ignored today for all non-Python repos. This convention closes that gap.

**Suggested fix**

Add a `docker` entry to `.github/dependabot.yml` for each directory that contains a built-service Dockerfile. For a repo with a single Dockerfile at the root:

```yaml
updates:
  - package-ecosystem: docker
    directory: /
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
```

For repos with multiple Dockerfiles (e.g. `api/Dockerfile` and `worker/Dockerfile`), add a separate entry for each directory, or use the `directories:` plural form:

```yaml
  - package-ecosystem: docker
    directories: ["/api", "/worker"]
    schedule:
      interval: weekly
    allow:
      - dependency-type: all
    groups:
      minor-and-patch:
        update-types: [minor, patch]
      major:
        update-types: [major]
```

The `allow: dependency-type: all` and `groups:` blocks are required by the `dependabot-configured` convention, which will flag their absence independently.

---

## `docker-healthcheck-on-built-services`

Every service with a build: key in docker-compose.yml also defines a healthcheck:

- **Applies to:** system

**Why this matters**

Without a Docker healthcheck, `docker compose up -d` returns as soon as the container *starts*, not when it is ready to serve traffic. The deploy suppression mechanism in lucos_monitoring clears suppression at that moment — meaning monitoring polls `/_info` before the process is listening, causing a consistent blip after every deploy. Adding a healthcheck makes Docker wait until the service is actually healthy before signalling readiness.

**Suggested fix**

Add a `healthcheck:` block to every service in `docker-compose.yml` that has a `build:` key. For HTTP services, a suitable target is the `/_info` endpoint, for example:

```yaml
healthcheck:
  test: ["CMD", "curl", "-sf", "http://127.0.0.1:${PORT}/_info"]
  interval: 10s
  timeout: 5s
  retries: 3
  start_period: 15s
```

Use `127.0.0.1` rather than `localhost`. Inside Alpine and Debian-based containers, `localhost` resolves to `::1` (IPv6) before `127.0.0.1` (IPv4), but services typically bind to `0.0.0.0` (IPv4 only). This causes the healthcheck to receive "Connection refused" on the IPv6 address and report the container as unhealthy even when the service is externally reachable.

Ensure the tool you invoke in the `test` command (`curl`, `wget`, etc.) is actually installed in the container image — the healthcheck runs inside the container, not on the host.

Off-the-shelf images (redis, postgres, etc.) are excluded — this rule only applies to services your repo builds from a Dockerfile.

---

## `dockerfile-copy-from-dependabot-blind`

No Dockerfile uses COPY --from=<external-image> without declaring that image as a named FROM stage, which would hide it from Dependabot

- **Applies to:** system

**Why this matters**

Dependabot's Docker ecosystem parser only scans FROM instructions for update PRs. A COPY --from=<external-image> that is not backed by a matching FROM <image> AS <stage> instruction is completely invisible to Dependabot — no version-bump PRs are ever opened, no error is shown, and the pinned image silently goes stale. This is a security-relevant gap: a shared data or artifact image pinned this way will never receive vulnerability-bump PRs. The same blind spot applies to FROM ${VAR} (ARG-in-FROM) and to additional_contexts entries that reference images via docker-image:// URLs.

**Suggested fix**

Declare the image as a named build stage and reference that stage name in COPY:

```dockerfile
# Instead of:
COPY --from=registry.example.com/my-image:1.2.3@sha256:abc... /src /dst

# Use:
FROM registry.example.com/my-image:1.2.3@sha256:abc... AS my-stage
# ... other build steps ...
COPY --from=my-stage /src /dst
```

Use both a tag AND a digest (image:tag@sha256:...) — a tag alone is mutable, a digest alone tracks latest with no semver signal. Dependabot bumps images that carry both a tag and a digest in a FROM instruction.

---

## `dockerfile-exposes-version`

Every built service Dockerfile declares ARG VERSION and ENV VERSION=$VERSION, and its docker-compose image: tag uses ${VERSION:-latest}

- **Applies to:** system

**Why this matters**

The deploy orb sets VERSION at build time via `VERSION=$NEXT_VERSION docker compose build`. For the running container to report its own version (e.g. via the `/_info` endpoint), the build arg must be declared with `ARG VERSION` and then persisted as an environment variable with `ENV VERSION=$VERSION`. For the built image to be pushed to Docker Hub with a versioned tag (e.g. `lucas42/lucos_foo:1.2.3`), the `image:` field in `docker-compose.yml` must also include `${VERSION:-latest}` as the tag. Without both the Dockerfile instructions and the compose image tag, versioned Docker Hub images are never produced and rollback is not possible.

**Suggested fix**

Add the following two lines to every service Dockerfile, after the FROM instruction and before the COPY/RUN steps:

```dockerfile
ARG VERSION
ENV VERSION=$VERSION
```

Note that Docker build args are scoped — if your Dockerfile uses multi-stage builds, you may need to repeat `ARG VERSION` in each stage that needs it.

Also add or update the `image:` field for every built service in `docker-compose.yml` to include `${VERSION:-latest}` as the tag:

```yaml
image: lucas42/lucos_myservice_web:${VERSION:-latest}
```

The `:-latest` default means `docker compose up` still works locally without setting VERSION, while the deploy orb's `VERSION=$NEXT_VERSION docker compose build` produces a properly versioned tag.

---

## `env-var-passthrough`

Every env var read by application code is declared as passthrough in docker-compose.yml

- **Applies to:** system

**Why this matters**

Docker Compose only forwards variables listed in a service's `environment:` block into the container. A variable read by application code but absent from that block is silently empty at runtime — the feature breaks without any error or alert. This was the root cause of the 2026-05-13 monitoring blackout (`lucos_monitoring#234`), where `SCHEDULE_TRACKER_ENDPOINT` was read in code but never added to `docker-compose.yml`.

**Suggested fix**

Add each missing variable as a bare passthrough entry in the `environment:` block of `docker-compose.yml`. For example:

```yaml
environment:
  - PORT
  - MY_VAR
```

If the variable is set to a hardcoded value directly in compose (`MY_VAR=fixed`), that is intentional config — no change needed. Test files and runtime-supplied OS/shell variables (`HOME`, `PATH`, `HOSTNAME`, `LC_*`, etc.) are excluded from scanning automatically. For any other variable that is genuinely not a compose concern, add a `# lucos_repos: noenv MY_VAR` annotation (using the appropriate comment syntax for the language) to the line where it is read.

---

## `fork-pr-contributor-approval`

Repositories must use the "first_time_contributors_new_to_github" GitHub Actions fork pull request contributor approval policy

- **Applies to:** all repo types

**Why this matters**

The default policy ("first_time_contributors") gates GitHub Actions workflow runs on any PR from an account that has not previously contributed to the repo — including lucos agent bots on newly-created repos. This causes the `code-reviewer-auto-merge.yml` workflow to be blocked behind a manual "Approve and run" click. The "first_time_contributors_new_to_github" policy exempts established GitHub accounts (including all lucos agent bots) while still requiring approval for brand-new GitHub accounts. See lucos_contacts#690 for the original blocking incident.

**Suggested fix**

Apply the correct policy via the GitHub API:

```
PUT /repos/{owner}/{repo}/actions/permissions/fork-pr-contributor-approval
{"fork-pr-contributor-approval": "first_time_contributors_new_to_github"}
```

Or use the lucos_agent_coding_sandbox#75 script to apply it estate-wide.

---

## `has-codeql-workflow`

Repository has a .github/workflows/codeql-analysis.yml workflow file

- **Applies to:** system, component

**Why this matters**

All lucos repos with meaningful application code should have a CodeQL analysis workflow to catch security vulnerabilities automatically. Without it, the repo is flying blind on code-level security issues.

**Suggested fix**

Add a `.github/workflows/codeql-analysis.yml` file to the repository. Use the GitHub-provided CodeQL starter workflow as a base, configuring it for the languages used in the repo.

---

## `in-lucos-configy`

Repository appears in lucos_configy under exactly one of the following types: system, component, or script

- **Applies to:** all repo types

**Why this matters**

lucos_configy is the central configuration store that powers monitoring, deployments, and other infrastructure tooling. A repo that is not listed in configy is invisible to these systems, which can lead to missed alerts, failed deploys, or incomplete inventory. A repo listed under more than one type is a configuration error that can cause unpredictable behaviour in tooling that relies on a single authoritative type.

**Suggested fix**

Add the repository to lucos_configy by editing the appropriate YAML file in the lucos_configy repo:

- **system** (`config/systems.yaml`): a service that is deployed and runs continuously (e.g. an API, a web app, a worker). Most lucos repos are systems.
- **component** (`config/components.yaml`): a shared library or reusable piece of infrastructure that is not deployed independently (e.g. a shared npm package, a base Docker image).
- **script** (`config/scripts.yaml`): a tool or script designed to run locally rather than being deployed to a server (e.g. a CLI tool, a migration script).

Each entry needs at minimum an `id` field matching the repository name (without the `lucas42/` prefix). If the repo is already listed under more than one type, remove the duplicate entries so it appears under exactly one.

---

## `no-stale-codeql-requirement-on-infra-repos`

Infrastructure-only repos must not have a required Analyze (X) CodeQL status check on main

- **Applies to:** system, component
- **Scheduled sweeps only** (skipped during PR audits)

**Why this matters**

If an infrastructure-only repo (Dockerfile, shell, config, etc.) carries a required `Analyze (X)` status check on main, CodeQL will never produce that check run — because the language isn't supported — and every Dependabot PR will be silently blocked from auto-merging indefinitely. This is exactly what happened to `lucos_private` and `lucos_static_media` on 2026-04-10 (see ADR-0005). The existing `required-status-checks-coherent` convention only fires when `HasCodeQLLanguage()` is true; this convention closes the symmetric gap for repos where it is false.

**Suggested fix**

Remove the stale `Analyze (X)` required status check from the branch protection rules for `main`. Go to Settings → Branches → Branch protection rules → Edit the rule for `main`, then delete the offending check from the "Require status checks to pass before merging" list. See ADR-0005 (docs/adr/0005-codeql-policy-by-repo-class.md) for the full policy context.

---

## `required-status-checks-coherent`

Required status checks on main are internally consistent: no stale names, CodeQL is required when applicable, and all checks fire on Dependabot PRs

- **Applies to:** system, component
- **Scheduled sweeps only** (skipped during PR audits)

**Why this matters**

Three separate failure modes can silently block all PRs or all Dependabot PRs from merging:

1. Stale check names — required checks that no longer fire on HEAD of main (e.g. after GitHub renamed CodeQL checks from 'Analyze (X)' to 'CodeQL'). These cause zero visible errors but prevent all merges.
2. Missing CodeQL requirement — without a required Analyze (X) check, auto-merge can complete before CodeQL finishes, allowing security findings to be silently ignored at merge time.
3. Dependabot-unsatisfiable checks — a required check that doesn't fire on Dependabot PRs permanently blocks auto-merge for all dependency updates.

Diagnosing these separately meant fixing one issue often broke another. A unified check surfaces all three problems at once and provides a coherent remediation plan.

**Suggested fix**

Review the repository's Settings → Branches → Branch protection rules for `main`.

For stale check names: remove or rename required checks that no longer appear in the Checks tab of a recent commit to main.

For missing CodeQL: add the CodeQL check run name (e.g. `Analyze (go)` or `Analyze (python)`) as a required status check. Ensure `.github/workflows/codeql-analysis.yml` uses an explicit language matrix.

For Dependabot-unsatisfiable checks: either switch from GitHub's 'default setup' CodeQL to a workflow-based setup with a `pull_request` trigger, or remove the unsatisfiable check from the required checks list.

---

## `reusable-workflow-pinned`

Reusable workflow references to lucas42/.github are pinned to a full commit SHA or a semver tag

- **Applies to:** system, component, script
- **Excluded repos:** lucas42/.github

**Why this matters**

Referencing reusable workflows with a mutable branch ref like `@main` means any commit pushed to the upstream repo is immediately picked up by all consumer workflows. If an attacker gains push access to lucas42/.github they can modify a shared workflow to exfiltrate secrets (notably the code-reviewer GitHub App private key) from the next workflow run in every consumer repo.

Two pinning strategies are accepted:
- **Full commit SHA** (`@<40-char-hex>`): immutable and auditable — the reference can never silently change. Use for one-off pins where Dependabot propagation is not needed.
- **Semver tag** (`@vX.Y.Z`): updated by Dependabot automatically when lucas42/.github publishes a new release. Tags are created by the release workflow only after smoke tests pass, so updates are gated. The `@vX.Y.Z` constraint prevents branch refs — Dependabot will open a PR for each new tag, which auto-merges via the standard workflow.

Short tags like `@v1` or bare branch refs like `@main` are not accepted.

**Suggested fix**

Update the `uses:` line in your caller workflow to use either a full SHA or a semver tag:

```yaml
# Option A: pinned to semver tag (recommended — Dependabot keeps it updated)
uses: lucas42/.github/.github/workflows/reusable-dependabot-auto-merge.yml@v1.0.0

# Option B: pinned to full commit SHA (immutable, no auto-updates)
uses: lucas42/.github/.github/workflows/reusable-dependabot-auto-merge.yml@<full-commit-sha>
```

To find the latest semver tag on lucas42/.github:
```
gh api repos/lucas42/.github/tags --jq '.[0].name'
```

To find the current SHA of lucas42/.github's main branch:
```
gh api repos/lucas42/.github/commits/main --jq '.sha'
```

---

## `standard-env-vars-in-compose`

Standard env vars referenced in code are declared in docker-compose.yml

- **Applies to:** system

**Why this matters**

Several incidents have been caused by a service implementing a feature that reads an env var, but `docker-compose.yml` not passing that var through to the container. The result is silent failure at runtime — the feature simply doesn't work, and there's no error to alert on. This convention catches the missing declaration before it causes a silent production failure.

**Suggested fix**

Add the missing env var(s) to the `environment:` block in `docker-compose.yml`. For example:

```yaml
environment:
  - PORT
  - LOGANNE_ENDPOINT
```

If the service genuinely does not need the var at runtime (e.g. it only appears in test code or documentation), consider whether the code should be restructured to make that clearer.
