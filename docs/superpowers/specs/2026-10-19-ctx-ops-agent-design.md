# ctx ops agent (`ctx mcp`) and the start of the Go port

Status: draft for approval · 2026-10-19

## Why

Most of the maintainer's day-to-day client work is the same handful of operations, typed by hand with details kept in their head. For one client (MTN, Kubernetes on TKGI), shell history shows:

- ~210 logins as two steps every time: an alias (`tkgi_preprod`, `tkgi_prod`, `tkgi_sandbox`) that runs `tkgi login -a <api> -u <user> -k` with the password put on the clipboard, then `tkgi get-credentials nonprod|prod|sandbox`.
- Finding a service from a nickname: `k get deploy -n madapi-preprod | grep kyc`. People say "subs", "kyc", "otm", "cms"; the deployments are `subscription-cis-system`, `customer-kyc-verification-aggregator`, …
- Tracing a request: logs from one pod or all pods of a deployment, current and `--previous`, grepped for an MSISDN/customer id with `-C N` context, sometimes saved to a file or the clipboard. This is the most common task.
- Restarts: `k rollout restart deploy/X -n madapi-<env>` then `rollout status`.
- Downstream checks: `network_tool_pp <script.sh> [namespace]` pipes a local script into the `network-tool` pod (`network-utility` namespace, label `app=network-tool`), plus manual `exec` into that pod. Scripts like `confirm-linking.sh`, `delinking.sh`, `provision-bundle.sh`, `bundle-purchase.sh`.
- Deployment details: `get deploy X -o yaml`, `describe`, working out where a config value comes from.
- Port-forwards, e.g. `svc/config-server 9090:8080`.

The same history mixes other clients (an AKS cluster via `kubectx acs-aks-cluster`, a homelab). The login password currently sits in plain text in `~/.zshrc`.

The goal: in Claude Code, Cursor or a similar agent, inside a client's folder, the maintainer says "restart subs on preprod", "trace 233… in kyc and subs on prod for the last 30 min", "export the logs", "what did the downstream return for A vs B", and the agent does it correctly for **that client** using **that client's** login and conventions. When they fix something new, the agent records how, so next time it's one sentence.

ctx already owns per-client identity and secrets. This extends it from "who am I for this client" to "how do I operate this client's systems".

## Decisions already made (by the maintainer)

1. **Where it's used:** inside Claude Code, Cursor or a similar MCP-capable agent. ctx does not ship its own chat or LLM calls.
2. **Who it's for:** the maintainer, for speed. It is a generic ctx feature: nothing client-specific is hard-coded; anyone can describe their own clients.
3. **Credentials:** always from the ctx secret store (`ctx secret`). Never in knowledge files, never returned to the agent.
4. **Prod vs nonprod:** prod work is the same as nonprod: logs, restarts / rollout restarts, deployment details, tracing where values come from.
5. **Order:** this MCP server and the knowledge store are the **first Go component** of the Go port. The Bash CLI keeps working; other commands move to Go later, one at a time.
6. **No client's tooling is built in.** TKGI is how one client (MTN) works. ctx must not have tkgi-specific code. Each client's login steps are written in that client's own config, and MTN's tkgi commands are just an example of that config.

## Goals

- `ctx mcp`: an MCP server (stdio) exposing per-client ops tools, scoped to the profile that owns the agent's working directory.
- A per-profile **knowledge store** of plain, hand-editable files: clusters/environments, services and nicknames, recipes (runbooks), network-test scripts.
- One-step login per environment, using the login steps the user wrote for that client's cluster and secrets from ctx. Credentials land in a **per-cluster kubeconfig file** that ctx owns. Never change `~/.kube/config` or the global current-context.
- The agent can **save what it learned** (new recipe, new service nickname) through tools, so knowledge grows as work happens.
- An **activity log** of every tool call per profile, so "what did I do on prod yesterday" is answerable and changes are auditable.
- Go foundations reusable by later port steps: profile `.conf` reader, path → profile resolver, secret access, a Go binary wired into `bin/ctx`, built and shipped by CI/installer.

## Non-goals

- No ctx-owned chat UI or LLM API calls.
- No team sharing or sync of knowledge (the files are plain, so a user can put them in git themselves).
- No porting of other ctx commands in this project. `use`, `setup`, `guard` etc. stay Bash.
- Kubernetes plus a generic command escape hatch only. No AWS/GCP/Azure ops tools yet.
- No native Windows.

## Anti-goals (failure even if it "works")

- **Acting on the wrong cluster or environment.** This is the worst outcome. Every kube command must carry an explicit kubeconfig and namespace derived from the named environment. Never rely on ambient current-context.
- **Changing state the user didn't see.** Restarts and any other change are visible to and approved by the user before running.
- **Secrets reaching the agent:** passwords, tokens, or Kubernetes Secret values in tool output or the activity log.
- **One client's knowledge, clusters or logs visible in another client's folder.**
- **Breaking other terminals:** switching kubectl contexts globally, or overwriting the user's `~/.kube/config`.
- Knowledge locked in an opaque format the user can't read or fix by hand.

## How it works

### Shape

```
Claude Code / Cursor ──stdio MCP──▶ ctx mcp  (Go binary)
                                       │ resolves profile from cwd (fail closed)
                                       ├─ reads ~/.ctx/profiles/<p>.conf
                                       ├─ reads/writes ~/.ctx/knowledge/<p>/
                                       ├─ secrets via `ctx secret get <p> <KEY>`
                                       └─ runs login steps, then kubectl with --kubeconfig + -n
```

Registered once at user scope (`claude mcp add --scope user ctx -- ctx mcp`, Cursor's global `~/.cursor/mcp.json`). `ctx mcp install claude|cursor` does this. The server resolves the profile using the same rules as autoswitch (longest `WORK_DIR` prefix, sibling rule, `.ctx` override). It uses the folders the host reports (MCP roots) when available and falls back to its working directory. If the roots belong to more than one profile, it fails closed. `--profile NAME` overrides. Claude Code is the target for v1. Cursor should work through the same roots mechanism but is not acceptance-tested yet (my call). **If no profile matches, the server exposes only a `client_context` tool that explains no client is active. It never falls back to the globally active profile.**

### Knowledge store layout

`~/.ctx/knowledge/<profile>/` (mode `0700`):

```
clusters.yaml        how to log in to each cluster
environments.yaml    env name → cluster + namespace (+ aliases)
services.yaml        service name → workload, nicknames
recipes/*.md         runbooks the agent follows
scripts/*.sh         scripts for network_test
activity.jsonl       append-only audit of tool calls (written by ctx)
exports/             full log exports and large outputs (written by ctx)
```

Example for the MTN case (values illustrative):

```yaml
# clusters.yaml  (MTN's file; TKGI is just what this client uses)
clusters:
  nonprod:
    login:
      type: command              # command | kubeconfig
      secrets: [TKGI_PASSWORD]   # keys in ctx secret store, given to the steps as env vars
      run:                       # run in order with KUBECONFIG=<ctx-owned file for this cluster>
        - tkgi login -a ac2-tkgi-api-nonprod.example.net -u Isaac.Kumi -p "$TKGI_PASSWORD" -k
        - tkgi get-credentials nonprod
  prod:
    login:
      type: command
      secrets: [TKGI_PASSWORD]
      run:
        - tkgi login -a ac2-tkgi-api-prod.example.net -u Isaac.Kumi -p "$TKGI_PASSWORD" -k
        - tkgi get-credentials prod
```

Another client's file might be a single `az aks get-credentials …` step, or `type: kubeconfig` pointing at a context that already exists.

```yaml
# environments.yaml
environments:
  dev:     { cluster: nonprod, namespace: madapi-dev }
  preprod: { cluster: nonprod, namespace: madapi-preprod, aliases: [pp, nonprod] }
  prod:    { cluster: prod,    namespace: madapi-prod,    protected: true }
network_tool:                     # optional, per environment override allowed
  namespace: network-utility
  selector: app=network-tool
  container: network-tool
```

```yaml
# services.yaml
services:
  subscription-cis-system: { kind: deployment, aliases: [subs, subscription, cis] }
  customer-kyc-verification-aggregator: { kind: deployment, aliases: [kyc, kyc-verify] }
```

Login types (the only two; nothing vendor-specific):

- `command`: steps written by the user. Each step runs with `sh -c`, with `KUBECONFIG` set to the ctx-owned file for that cluster and each listed secret as an env var. ctx never pastes secret values into the step text itself; the step references `$NAME`. After the steps run, ctx checks that the kubeconfig file exists and that `kubectl version` against it works.
- `kubeconfig`: use an existing kubeconfig file and context (`path`, `context`). Covers AKS and homelab clusters already in `~/.kube/config`. ctx copies or references them read-only and still passes `--context` explicitly.

**Recipes** are markdown with front matter. They are instructions the agent reads and follows using the tools. ctx does not execute them as scripts.

```markdown
---
name: trace-msisdn
description: Find every log line for an MSISDN across services, current and previous pods
tags: [trace, logs]
params: [msisdn, services, env, since]
changes_state: false
---
1. For each service, call `logs` with `all_pods: true, previous: true, grep: "{msisdn}", context: 10, since: {since}`.
2. If the request path is `/customers/{id}/subscriptions`, also check `subscription-aggregator-v2`.
3. Export the merged result with `export: true` and give the user the file path.
```

The store starts empty. `ctx knowledge init [profile]` scaffolds the three YAML files with comments. Services can also be filled by the agent: `discover_services` lists workloads, then `save_service` stores the nicknames the user confirms.

### Login and kubeconfig isolation

- `login(env)` resolves env → cluster and runs that cluster's login steps with `KUBECONFIG=~/.ctx/kube/<profile>/<cluster>.yaml`, so credentials land in ctx's own file. Logins for one profile are serialized with a lock file, since a vendor CLI may keep global login state. Step output goes through redaction before it is returned.
- MTN-specific note, for acceptance only: tkgi keeps its own login state outside `KUBECONFIG` and may prompt for a password during `get-credentials`. Making MTN's steps non-interactive is part of writing MTN's `clusters.yaml`, not ctx code.
- Every kubectl call made by ctx uses `--kubeconfig <ctx file>` (plus `--context` for the `kubeconfig` type) and `-n <namespace>`. `KUBECONFIG` in the agent's environment is ignored.
- If a call fails with an auth/expired error, the tool returns a clear "run login(env)" result. Auto-re-login once is the builder's call.

### Tools

Each tool takes `env` (name or alias) where relevant. Each tool is marked with MCP annotations (`readOnlyHint` / `destructiveHint`). Read tools are separate from change tools so the user can allow read tools without prompts in the host and keep prompts for changes.

**Read tools (run without asking):**

| Tool | Does |
|------|------|
| `client_context` | Profile name, environments, services (with aliases), recipe index (name + description), network-tool config. The agent calls this first. |
| `login` | Login as above. Counts as read: it changes only ctx-owned files. |
| `logs` | `service`, optional `pod`, `all_pods`, `previous`, `container`/`all_containers`, `since`, `tail`, `grep` (regex), `context` lines, `export`. Merges pods with a pod-name prefix on each line. Output capped (default ~400 lines); full output is written to `exports/` and the path returned when capped or when `export: true`. |
| `trace` | `pattern`, `services[]`, `since`, `previous`. Runs `logs` across several services in parallel. Results are grouped by service, and sorted by timestamp when lines start with a parseable timestamp. |
| `describe_service` | Workload summary: image, replicas/ready, rollout status, recent events, resources, probes, and **env var sources**: literal values, ConfigMap key refs (values shown), Secret key refs (**name and key only, never value**), `envFrom`. Also mounted ConfigMaps. Answers "where does this value come from". |
| `kubectl_read` | Escape hatch for read-only kubectl: verbs `get, describe, logs, top, events, explain, api-resources, rollout status, rollout history, auth can-i`. `get secret(s)` with `-o yaml/json` has `data` values redacted. |
| `discover_services` | Lists deployments/statefulsets in the env namespace, with existing aliases. |
| `search_knowledge` | Text search over recipes and YAML. |
| `get_recipe` | Full recipe body. |
| `activity` | Recent activity log entries, filterable by env/tool/date. |

**Change tools (the host asks the user before running):**

| Tool | Does |
|------|------|
| `restart` | `rollout restart` for the service in env, then waits on `rollout status` (timeout) and reports result. |
| `network_test` | Runs a script from `scripts/` (or inline script text) in the network-tool pod via `exec -i … -- /bin/sh -s`, same as `network_tool_pp`. Marked as a change because the script can call downstream systems. Scripts need no credentials (maintainer confirmed), so this is a plain pipe with no secret injection. Output still goes through redaction. |
| `port_forward` | Start/stop/list background port-forwards owned by the server. All are stopped when the server exits. |
| `kubectl_change` | Any other kubectl verb (scale, patch, delete, set image, exec…). Always destructive. |
| `save_recipe` | Create or update `recipes/<name>.md`. |
| `save_service` | Add a service or nicknames to `services.yaml`. |
| `save_script` | Add a script to `scripts/`. |

Saving tools are on the change list so the user sees what gets written. Edits to `clusters.yaml` / `environments.yaml` are left to the user (hand edit or `ctx knowledge init`) to keep the agent away from login config.

**ctx asks the user itself before every change tool runs (maintainer's decision).** The host's own permission prompt is not enough: auto mode or "always allow" rules can skip it.

- The server sends an MCP elicitation request to the host, which shows the user a confirmation. It contains the plan: profile, environment, cluster, namespace, and the exact command(s) ctx will run (redacted). Only an explicit accept runs it. Decline, cancel or timeout returns "not run" to the agent.
- If the host doesn't support elicitation, change tools do not run. They return the exact command for the user to run themselves. A confirm-token round trip is rejected because the agent could approve its own token.
- The builder must verify elicitation support in the current Claude Code release before building on it. If it's missing, stop and report back rather than switching to a weaker mechanism.
- For `protected: true` environments, change tools also require the `env` argument to be the canonical name (not an alias). Mis-routing by nickname then can't reach prod.

### Safety rules (apply to all tools)

- **Redaction:** before returning any output or writing activity, replace every secret value known to the profile (`SECRET_KEYS`, login passwords) with `<redacted>`. Kubernetes Secret `data`/`stringData` values are always redacted.
- **Scoping:** a tool can only reach the clusters, knowledge and secrets of the resolved profile. Names that don't resolve to that profile's env or service are errors. They are never guessed across profiles.
- **Unknown env or service:** return the list of valid names and aliases. Never pick a "closest" one for change tools.
- **Activity log:** `activity.jsonl` gets one line per call: timestamp, tool, env, cluster, namespace, args (redacted), the exact kubectl/tkgi argv run (redacted), exit status, duration. No log output bodies.

### Go component

- Go module at the repo root (`go.mod`), binary `ctx-core` (renamed `ctx` when the port completes). `cmd/ctx-core/`, packages under `internal/` (`profile`, `resolve`, `secret`, `knowledge`, `kube`, `mcpserver`).
- `bin/ctx` dispatches `mcp` and `knowledge` to `ctx-core`, found next to the lib dir or on `PATH`, with a clear error if missing. Everything else stays in Bash.
- **Profile reader:** parses the assignment-only `.conf` files ctx writes (`KEY="…"`, `KEY='…'`, multi-line double-quoted values like `EXTRA_ENVS`, `$HOME`/`${HOME}`/`~`). Same fallback rule as the autoswitch hook: values needing other shell expansion are read via `bash -c 'source …'`.
- **Resolver:** Go port of `ctx_resolve_path_with_root`. It must pass the same cases as `test_resolve_path_profile` and the autoswitch hook tests (shared fixtures).
- **Secrets:** call `ctx secret get <profile> <KEY>` (the Bash CLI) so provider logic isn't duplicated yet.
- MCP library: the official Go SDK (`github.com/modelcontextprotocol/go-sdk`) unless the builder finds a blocker. YAML library: builder's call.
- Current stable Go. The maintainer's machine has 1.21, which must be upgraded.

### Build, install, CI

- CI: `go vet`, `go test ./...`, and a build for darwin/linux × amd64/arm64 alongside the existing ShellCheck + `test/test.sh` jobs.
- Release: tagged releases attach `ctx-core` binaries (goreleaser or a plain matrix, builder's call).
- `install.sh`: downloads the matching `ctx-core` release asset next to the libs, or builds it from source when run from a checkout with Go available. `ctx doctor` reports whether `ctx-core` is present and its version. The Homebrew formula builds it with Go.

### Testing

- Unit tests for the profile reader, resolver (shared fixtures with Bash tests), knowledge loading/validation, redaction, alias resolution, and protected-env rules.
- Tool tests using **fake `kubectl` and login-step executables** on `PATH` that record argv and print canned output. Assert the exact argv (always `--kubeconfig` and `-n`), redaction, capping/export, and that `~/.kube/config` is never touched.
- An MCP-level test that starts the server over stdio, lists tools, and calls `client_context` + `logs` against the fakes.
- Elicitation tests with a test MCP client: accept runs the command, decline/cancel/timeout runs nothing, and a client without elicitation gets the "run it yourself" result with nothing executed.
- No real cluster in CI. Manual acceptance on the maintainer's MTN setup (see below).

## Acceptance (manual, on the maintainer's machine)

1. Move the tkgi password into `ctx secret set mtn TKGI_PASSWORD`; remove it from `.zshrc`.
2. `ctx knowledge init mtn`, fill clusters/environments, `ctx mcp install claude`.
3. In Claude Code under the MTN folder: "log in to preprod", "restart subs on preprod" (ctx's confirmation appears even in auto mode, then rollout status is reported), "trace an MSISDN in kyc and subs on prod for the last hour and export it", "where does SUBSCRIPTION_URL in subs come from on prod", "run confirm-linking on preprod network tool".
4. Teach a new fix, and confirm it shows up as a recipe and is used next session.
5. Open Claude Code in another client's folder: none of MTN's environments, services, recipes or activity are visible. Open it outside any client: no ops tools.
6. In another terminal, `kubectl config current-context` is unchanged throughout.

## Left to the builder

- Package layout details, YAML/CLI libraries, release tooling.
- Output cap sizes, timeouts, port-forward lifecycle details.
- Timestamp parsing heuristics for `trace`.
- Whether `login` auto-retries on expired credentials.
- Exact wording of tool descriptions (they matter to agent behavior; keep them short and precise).

## Out of scope, noted for later

- `ctx use` exports (`AWS_PROFILE`, secrets) don't reach the calling shell because ctx runs as a child process. This is a separate fix, likely folded into the Go `use`.
- Per-terminal gcloud/az/kubectl for `ctx use` and mise hooks (`CLOUDSDK_ACTIVE_CONFIG_NAME`, `AZURE_CONFIG_DIR`, per-profile `KUBECONFIG`).
- Porting the remaining commands to Go.
