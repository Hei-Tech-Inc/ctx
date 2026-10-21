# ctx ops agent (`ctx mcp`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go binary `ctx-core` whose `mcp` command is an MCP server that gives Claude Code per-client Kubernetes ops tools (login, logs, trace, describe, restart, network test, port-forward, save knowledge), scoped to the ctx profile that owns the working folder, with ctx itself confirming every change.

**Architecture:** New Go module at the repo root. `bin/ctx` hands `mcp` and `knowledge` to `ctx-core`; all other commands stay in Bash. `ctx-core` reads the existing profile `.conf` files and the new per-profile knowledge store `~/.ctx/knowledge/<profile>/`. It gets secrets by calling `ctx secret get`, and runs the user's login steps and `kubectl` with a ctx-owned kubeconfig plus an explicit namespace.

**Tech Stack:** Go (current stable, ≥1.24), `github.com/modelcontextprotocol/go-sdk/mcp`, `gopkg.in/yaml.v3`, stdlib otherwise. Bash tests stay in `test/test.sh`.

**Spec:** [docs/superpowers/specs/2026-10-19-ctx-ops-agent-design.md](../specs/2026-10-19-ctx-ops-agent-design.md). Read it before any task.

## Global Constraints

- Module path `github.com/Hei-Tech-Inc/ctx`; binary `ctx-core`; main at `cmd/ctx-core/`; packages under `internal/`.
- No vendor-specific code (no tkgi, aks, etc.). Login types are exactly `command` and `kubeconfig`.
- Every kubectl invocation built by ctx carries `--kubeconfig <file>` and `-n <namespace>`, plus `--context <ctx>` for `kubeconfig` logins. `KUBECONFIG` from the environment is never used, and `~/.kube/config` is never written.
- Knowledge root: `$CTX_DIR/knowledge/<profile>/` (`CTX_DIR` defaults to `~/.ctx`), dir mode `0700`, files `0600`. Kubeconfigs: `$CTX_DIR/kube/<profile>/<cluster>.yaml`.
- Secret values (every key in the profile's `SECRET_KEYS` plus every key named in any cluster's `login.secrets`) are replaced with `<redacted>` in all tool output, errors and activity entries. Kubernetes Secret `data`/`stringData` values are always `<redacted>`.
- Change tools run only after an MCP elicitation `accept`. If the host has no elicitation capability, they return the exact command for the user and execute nothing.
- No profile resolved → only the `client_context` tool, which says no client is active. Never fall back to `active=` in `~/.ctx/config`.
- Shell scripts stay ShellCheck-clean (`shellcheck -x -S warning bin/ctx lib/*.sh lib/*.bash install.sh test/*.sh`). `bash test/test.sh` keeps passing.
- Commits: no `Co-authored-by` or other vendor/agent trailers (CI `git-history-hygiene` rejects them; see CONTRIBUTING.md).

## Review Focus

1. **Ambiguous names:** an alias shared by two environments or two services, or an env alias equal to another env's canonical name. Loading must reject it with a message naming both entries. It must never pick one. Test: Task 4 `TestLoadRejectsDuplicateAlias`.
2. **Expired or missing credentials mid-session:** kubectl returns `Unauthorized`, `You must be logged in`, or the kubeconfig file is missing. The tool returns `not logged in to <env>: call login(env="<env>")`, not a raw dump. Test: Task 5 `TestRunMapsAuthErrors`.
3. **Huge logs:** a service printing hundreds of thousands of lines must not be held in memory. Stream to the export file and keep only the cap in memory. Test: Task 6 `TestLogsStreamsLargeOutput` (200k lines, returned text ≤ cap, export file has all lines).
4. **Broken hand-edited YAML:** a syntax error in `services.yaml`. The server still starts; `client_context` returns the file, line and error, and other tools return the same error. No crash, no silent empty tool list. Test: Task 7 `TestServerReportsBrokenKnowledge`.
5. **A secret echoed by a login step:** a vendor CLI prints the password or token on failure. That output must come back redacted. Test: Task 5 `TestLoginRedactsStepOutput`.

---

### Task 1: Go module, `ctx-core` binary, wiring into `bin/ctx`, CI

**Files:**
- Create: `go.mod`, `cmd/ctx-core/main.go`, `cmd/ctx-core/main_test.go`
- Modify: `bin/ctx` (dispatch + help), `.gitignore` (add `/bin/ctx-core`), `.github/workflows/ci.yml` (Go job), `test/test.sh` (dispatch test)

**Interfaces:**
- Produces: `ctx-core version` prints `ctx-core <version>`, where version is `-ldflags "-X main.version=…"`, defaulting to `dev`. `ctx-core` subcommand dispatch is a switch in `main.go` over `os.Args[1]`: `version`, `mcp`, `knowledge`. Unknown → usage on stderr, exit 2. Later tasks add cases.
- Produces: `bin/ctx` function `_ctx_core_bin` that prints the path of `ctx-core`, searching in order `$CTX_BIN/ctx-core`, `$CTX_LIB/ctx-core`, then `command -v ctx-core`. It returns 1 if none is found.

- [ ] **Step 1: Write failing tests.** `TestVersionCommand` runs `run([]string{"version"}, &out, &errOut)` and asserts out == `"ctx-core dev\n"`, exit 0. `TestUnknownCommand` asserts exit 2 and stderr contains `usage: ctx-core`. Make `main()` a thin wrapper over `func run(args []string, stdout, stderr io.Writer) int`. In `test/test.sh` add `test_ctx_core_dispatch`. With a fake `ctx-core` script in a temp dir on `CTX_BIN`-adjacent path that echoes its args, `bin/ctx mcp --x` prints `mcp --x`. With no ctx-core found, `bin/ctx mcp` exits 1 and stderr contains `ctx-core not found`.
- [ ] **Step 2: Run** `go test ./cmd/...` and `bash test/test.sh`. Expected: FAIL (no module / no dispatch).
- [ ] **Step 3: Implement.** `go.mod` (module path above, go directive = installed stable). `main.go` per Interfaces. In `bin/ctx` `main()`, add `mcp|knowledge) _ctx_core_exec "$cmd" "$@" ;;` where `_ctx_core_exec` runs `exec "$(_ctx_core_bin)" "$@"`, or prints `ctx-core not found — reinstall ctx (install.sh) or run: go build -o bin/ctx-core ./cmd/ctx-core` and exits 1. Add `mcp` and `knowledge` lines to both help texts and all three completions.
- [ ] **Step 4: CI.** Add job `go` to ci.yml: `actions/setup-go@v5` with `go-version-file: go.mod`, then `go vet ./...`, `go test ./...`, `go build ./cmd/ctx-core`, and a cross-build step for `GOOS` ∈ {darwin, linux} × `GOARCH` ∈ {amd64, arm64}. Add `go build -o bin/ctx-core ./cmd/ctx-core` before `bash test/test.sh` in `unit-tests`, with setup-go.
- [ ] **Step 5: Run** `go test ./...`, `bash test/test.sh`, shellcheck. Expected: all PASS.
- [ ] **Step 6: Commit** `feat(core): add ctx-core Go binary and bin/ctx dispatch`

### Task 2: Profile reader (`internal/profile`)

**Files:**
- Create: `internal/profile/profile.go`, `internal/profile/profile_test.go`, `internal/profile/testdata/*.conf`

**Interfaces:**
- Produces:
  - `type Profile struct { Name, GitName, GitEmail, WorkDir, GithubUser, SSHKeyPath, AWSProfile, AzureSubscription, AzureTenant, GCPProject, GCPAccount, KubeContext, SigningFormat, SigningKey string; SecretKeys []string; ExtraEnvs map[string]string }`, mapped from `PROFILE_NAME, GIT_NAME, GIT_EMAIL, WORK_DIR, GITHUB_USER, SSH_KEY_PATH, AWS_PROFILE_NAME, AZURE_SUBSCRIPTION, AZURE_TENANT, GCP_PROJECT, GCP_ACCOUNT, KUBE_CONTEXT, SIGNING_FORMAT, SIGNING_KEY, SECRET_KEYS` (space-separated) and `EXTRA_ENVS` (newline-separated `K=V`). `Name` is the file basename, not `PROFILE_NAME`.
  - `func Dir() string`: `$CTX_DIR/profiles`, default `$HOME/.ctx/profiles`.
  - `func Load(dir, name string) (*Profile, error)`
  - `func List(dir string) ([]string, error)`: sorted basenames of `*.conf`.

- [ ] **Step 1: Write failing tests** with testdata files:
  - `TestLoadQuoted`: `KEY="v"`, `KEY='v'`, bare `KEY=v`, last assignment wins, `#` comment lines, CRLF line endings.
  - `TestLoadHomeExpansion`: `WORK_DIR="$HOME/c"`, `"${HOME}/c"`, `"~/c"` all → `<HOME>/c` (set `HOME` via `t.Setenv`).
  - `TestLoadMultilineExtraEnvs`: `EXTRA_ENVS="A=1\nB=two words"` written as a real multi-line double-quoted value → `{"A":"1","B":"two words"}`.
  - `TestLoadFallsBackToBash`: `WORK_DIR="$HOME/$(echo x)"` → `<HOME>/x` (skip if `bash` not on PATH).
  - `TestLoadMissing`: error mentions the profile name.
  - `TestListSorted`.
- [ ] **Step 2: Run** `go test ./internal/profile/`. Expected: FAIL.
- [ ] **Step 3: Implement.** Line parser for `KEY=value` with single/double quotes and multi-line double-quoted values. Expand `$HOME`, `${HOME}` and a leading `~`. If any value still contains `$`, a backtick or a backslash after that, read the whole file with `bash -c 'source "$1" >/dev/null 2>&1; for k in …; do printf "%s\0%s\0" "$k" "${!k}"; done' _ <file>` and use those values. This is the same rule as `_ctx_as_profile_work_dir` in `lib/ctx_autoswitch.bash`.
- [ ] **Step 4: Run tests.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): read ctx profile .conf files in Go`

### Task 3: Path → profile resolver (`internal/resolve`) with Bash conformance

**Files:**
- Create: `internal/resolve/resolve.go`, `internal/resolve/resolve_test.go`, `internal/resolve/conformance_test.go`

**Interfaces:**
- Consumes: `profile.List`, `profile.Load` (Task 2).
- Produces: `func Path(pwd, profilesDir string) (name, workDir string, err error)`. Empty name with nil error means no match. This is a port of `ctx_resolve_path_with_root` (`lib/core.sh:426`): longest `WORK_DIR` prefix, then the sibling rule (first segment under the best prefix names an existing `<seg>.conf` whose dir exists), then the nearest `.ctx` with `profile=<name>` from `pwd` up to the git root (a dir containing `.git`, as the autoswitch hook does; no `git` subprocess).

- [ ] **Step 1: Write failing tests.** `TestPath` is table-driven over the same trees `test_ctx_resolve_path_profile` (test/test.sh:160) builds: nested prefix wins; sibling `clients/<name>` beats broad `clients` profile; `.ctx` override inside a repo; `.ctx` nearer to pwd beats one at the repo root; `.ctx` naming a missing profile is ignored; no match → `""`. Also `TestPathTrailingSlash`: `WORK_DIR="<td>/acme/"` matches `<td>/acme/app`.
- [ ] **Step 2: Write conformance test** `TestMatchesBash` (skip without `bash`). It builds the trees, then for each pwd compares `Path` with the output of `bash -c 'source lib/core.sh; ctx_resolve_path_with_root "$1" "$2"' _ pwd dir` (repo root found via `go env`/relative path `../../lib/core.sh`). Expect identical name and workDir. The trailing-slash case is excluded if Bash differs; record it in a comment.
- [ ] **Step 3: Run** `go test ./internal/resolve/`. Expected: FAIL.
- [ ] **Step 4: Implement** `Path`.
- [ ] **Step 5: Run tests.** Expected: PASS.
- [ ] **Step 6: Commit** `feat(core): port directory-to-profile resolution to Go`

### Task 4: Knowledge store (`internal/knowledge`) and `ctx knowledge init`

**Files:**
- Create: `internal/knowledge/types.go`, `load.go`, `save.go`, `search.go`, `activity.go`, `init.go`, and `*_test.go` for each; `internal/knowledge/templates/*.yaml` (embedded with `//go:embed`)
- Modify: `cmd/ctx-core/main.go` (`knowledge init [profile]`)

**Interfaces:**
- Produces:
  - Types (YAML tags match the spec's examples): `Store{Root string; Clusters map[string]Cluster; Envs map[string]Env; Services map[string]Service; NetworkTool NetworkTool}`; `Cluster{Login Login}`; `Login{Type string /* "command"|"kubeconfig" */; Secrets []string; Run []string; Path, Context string}`; `Env{Name, Cluster, Namespace string; Aliases []string; Protected bool; NetworkTool *NetworkTool}`; `Service{Name, Kind /* deployment|statefulset, default deployment */ string; Aliases []string}`; `NetworkTool{Namespace, Selector, Container string}`; `Recipe{Name, Description string; Tags, Params []string; ChangesState bool; Body string}`.
  - `func Root(profile string) string`: `$CTX_DIR/knowledge/<profile>`.
  - `func Load(root string) (*Store, error)`. A missing dir or missing files means an empty store with no error. Errors are `*LoadError{File string; Line int; Msg string}`.
  - `func (s *Store) Env(nameOrAlias string) (Env, error)`, `func (s *Store) EnvForChange(name string) (Env, error)` (protected env requires the canonical name), `func (s *Store) Service(nameOrAlias string) (Service, error)`. Unknown-name errors list the valid names with their aliases.
  - `func (s *Store) SecretKeys() []string`: union of all `Login.Secrets`.
  - `func (s *Store) Recipes() ([]Recipe, error)`, `GetRecipe(name)`, `Search(query string) ([]Hit, error)` where `Hit{File string; Line int; Text string}` is a case-insensitive substring match over recipes and the YAML files.
  - `func SaveRecipe(root string, r Recipe) (path string, err error)`, `SaveService(root string, svc Service) error` (merges aliases into existing), `SaveScript(root, name, body string) (path string, err error)`. Names must match `^[a-z0-9][a-z0-9-]{0,62}$` (scripts may end `.sh`). Writes are atomic (temp file + rename), modes per Global Constraints.
  - `type Activity struct{ path string }`; `func OpenActivity(root string) *Activity`; `func (a *Activity) Append(e ActivityEntry) error`; `func (a *Activity) Query(f ActivityFilter) ([]ActivityEntry, error)`. `ActivityEntry{Time time.Time; Tool, Env, Cluster, Namespace string; Args map[string]any; Argv [][]string; Exit int; DurationMS int64; Result string /* ok|error|declined|not_run */}`. `ActivityFilter{Env, Tool string; Since time.Time; Limit int}`.
  - `func Init(root string) ([]string, error)`: writes commented `clusters.yaml`, `environments.yaml`, `services.yaml` templates and `recipes/` `scripts/` dirs. It never overwrites existing files and returns the paths created.

- [ ] **Step 1: Write failing tests:**
  - `TestLoadSpecExample`: the spec's three MTN example files (copy them into testdata) load. `Env("pp").Name == "preprod"`, `Service("subs").Name == "subscription-cis-system"`, `SecretKeys() == ["TKGI_PASSWORD"]`.
  - `TestLoadEmptyDir`: empty store, nil error.
  - `TestLoadRejectsDuplicateAlias`: an alias on two envs, an alias on two services, and an env alias equal to another env's name each give a `LoadError` naming both entries.
  - `TestLoadRejectsBadReferences`: env with unknown cluster; login type `tkgi`; `command` login with empty `run`.
  - `TestLoadErrorHasLine`: YAML syntax error → `LoadError.File` = `services.yaml`, `Line` > 0.
  - `TestEnvForChangeProtected`: `EnvForChange("prod")` ok; `EnvForChange` on an alias of a protected env errors with `use the full environment name "prod"`; an alias of an unprotected env is ok.
  - `TestUnknownEnvListsChoices`.
  - `TestSaveRecipeRoundTrip`, `TestSaveRejectsBadName` (`../x`, `A B`), `TestSaveServiceMergesAliases`, `TestSaveModes` (0600/0700).
  - `TestActivityAppendQuery`: filter by env and since; `Limit` returns newest first.
  - `TestInitDoesNotOverwrite`.
  - `TestKnowledgeInitCommand` in `cmd/ctx-core`: `knowledge init acme` with `CTX_DIR` temp creates the files and prints their paths. With no profile arg it resolves from cwd via `resolve.Path`. An unknown profile → exit 1.
- [ ] **Step 2: Run** `go test ./internal/knowledge/ ./cmd/...`. Expected: FAIL.
- [ ] **Step 3: Implement.** Recipes are parsed as `---` YAML front matter plus a markdown body. Use `yaml.Node` decoding to recover line numbers for errors.
- [ ] **Step 4: Run tests.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): per-profile knowledge store and ctx knowledge init`

### Task 5: Secrets, redaction, kube runner and login (`internal/secret`, `internal/kube`)

**Files:**
- Create: `internal/secret/secret.go`, `secret_test.go`; `internal/kube/runner.go`, `login.go`, `readonly.go`, `redact_k8s.go` and tests; `internal/testutil/fakebin.go`

**Interfaces:**
- Consumes: `knowledge.Store`, `knowledge.Env`, `knowledge.Cluster` (Task 4), `profile.Profile` (Task 2).
- Produces:
  - `type Getter interface{ Get(profile, key string) (string, error) }`; `type CLIGetter struct{ CtxBin string }` runs `<CtxBin> secret get <profile> <key>` and trims one trailing newline. `CtxBin` defaults to `ctx` on PATH.
  - `type Redactor struct{…}`; `func NewRedactor(values []string) *Redactor` ignores values shorter than 4 chars and replaces longest first; `func (r *Redactor) String(s string) string`; `func (r *Redactor) Bytes(b []byte) []byte`.
  - `func LoadRedactor(p *profile.Profile, s *knowledge.Store, g Getter) *Redactor`: fetches every key in `p.SecretKeys` ∪ `s.SecretKeys()`. Missing keys are skipped.
  - `type Target struct{ Kubeconfig, Context, Namespace string }`; `func TargetFor(profileName string, s *knowledge.Store, env knowledge.Env) Target` (kubeconfig path from Global Constraints for `command`; `Login.Path` + `Login.Context` for `kubeconfig`).
  - `type Runner struct{ Kubectl string /* default "kubectl" */; Redact *secret.Redactor }`; `func (r *Runner) Args(t Target, args ...string) []string` returns `--kubeconfig <t.Kubeconfig> [--context <t.Context>] -n <t.Namespace>` + args. `-n` is omitted only when args contain `-A` or `--all-namespaces`. `func (r *Runner) Run(ctx context.Context, t Target, args ...string) (stdout []byte, err error)` sets env `KUBECONFIG=` (empty) for the child. Errors are `*RunError{Argv []string; Exit int; Stderr string}`, redacted. `func (r *Runner) Stream(ctx context.Context, t Target, w io.Writer, args ...string) error` is the streaming variant.
  - `var ErrNotLoggedIn` wrapped as `fmt.Errorf("not logged in to %s: call login(env=%q)", env, env)` when stderr matches `Unauthorized`, `You must be logged in`, `provide credentials`, `token has expired`, or the kubeconfig file is missing. `func AsAuthError(err error, env string) error`.
  - `func Login(ctx context.Context, profileName string, s *knowledge.Store, cluster string, g secret.Getter, red *secret.Redactor) (output string, err error)`. For `command`: takes an exclusive lock on `$CTX_DIR/kube/<profile>/.login.lock` (flock), runs each step with `sh -c <step>` and env = os env + `KUBECONFIG=<target file>` + each `Login.Secrets` key, stopping at the first failure. Then checks the file exists and `kubectl --kubeconfig <file> version --request-timeout=10s` succeeds. For `kubeconfig`: checks `Path` exists and `version` succeeds with `--context`. All output is redacted.
  - `func IsReadOnly(args []string) bool`: true only if the first non-flag arg is in `get, describe, logs, top, events, explain, api-resources, api-versions, version, cluster-info`, or the first two are `rollout status`, `rollout history` or `auth can-i`.
  - `func RedactSecretObjects(out []byte) []byte`: for `-o json|yaml` output of Secret objects or lists, replaces every `data`/`stringData` value with `<redacted>`.
  - `internal/testutil`: `func FakeBin(t *testing.T, name string, script string) (dir string, argvLog string)` writes an executable `name` into a temp dir that appends its argv (NUL-separated, one invocation per line) to `argvLog` and then runs `script`. `func ReadArgv(t, argvLog) [][]string`.

- [ ] **Step 1: Write failing tests:**
  - `TestRedactor`: longest-first; short values ignored; value inside a larger token replaced.
  - `TestCLIGetter` with a fake `ctx` that prints `s3cr3t\n` → `s3cr3t`.
  - `TestArgsAlwaysScoped`: `Args(t,"get","pods")` == `["--kubeconfig",f,"-n","ns","get","pods"]`; with Context → includes `--context`; with `-A` → no `-n`.
  - `TestRunClearsKUBECONFIG`: with `KUBECONFIG=/bad` set in the test env, a fake kubectl that prints `$KUBECONFIG` prints empty.
  - `TestRunMapsAuthErrors`: fake kubectl exits 1 with `error: You must be logged in to the server (Unauthorized)` → `errors.Is(AsAuthError(err,"preprod"), ErrNotLoggedIn)` and message `not logged in to preprod: call login(env="preprod")`. A missing kubeconfig file gives the same error.
  - `TestLoginRunsStepsWithEnv`: two steps. The fake records `$KUBECONFIG` and `$TKGI_PASSWORD`, and the second step creates the kubeconfig file; KUBECONFIG == `<CTX_DIR>/kube/mtn/nonprod.yaml` and the password is visible to the step; then `version` is called with `--kubeconfig`.
  - `TestLoginStopsOnFailure` and `TestLoginRedactsStepOutput` (a step that echoes `$TKGI_PASSWORD` and exits 1 → error text contains `<redacted>`, not the value).
  - `TestLoginNeverTouchesHomeKubeConfig`: `HOME` temp; after login, `$HOME/.kube/config` does not exist.
  - `TestIsReadOnly`: table including `rollout restart` → false, `rollout status` → true, `exec` → false, `delete` → false, `get secret x -o yaml` → true.
  - `TestRedactSecretObjects`: JSON Secret, JSON List of Secrets, YAML Secret; non-Secret objects unchanged.
- [ ] **Step 2: Run** `go test ./internal/secret/ ./internal/kube/`. Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces.
- [ ] **Step 4: Run tests.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): scoped kubectl runner, login steps, redaction`

### Task 6: Logs, trace and describe (`internal/ops`)

**Files:**
- Create: `internal/ops/logs.go`, `trace.go`, `describe.go`, `export.go` and tests

**Interfaces:**
- Consumes: `kube.Runner`, `kube.Target` (Task 5); `knowledge.Service` (Task 4).
- Produces:
  - `type LogsOpts struct{ Service knowledge.Service; Pod string; AllPods, Previous, AllContainers bool; Container, Since string; Tail int; Grep string /* RE2 */; Context int; Export bool; Cap int /* default 400 lines */ }`
  - `type LogsResult struct{ Text string; Lines int; Truncated bool; ExportPath string; Pods []string }`
  - `func Logs(ctx context.Context, r *kube.Runner, t kube.Target, exportDir string, o LogsOpts) (LogsResult, error)`. Pods come from the workload's `.spec.selector.matchLabels` (`get <kind>/<name> -o json`), then `get pods -l <k=v,…> -o json`. Each pod's `logs` (and, if `Previous`, `logs --previous`, ignoring "previous terminated container not found") is streamed. Every line is prefixed `<pod>[/<container>]: ` (plus ` (previous)`). The grep filter keeps `Context` lines before and after each match, with `--` between groups. The stream is written to an export file `exportDir/<UTC yyyymmdd-hhmmss>-<service>.log` when `Export` is set or output exceeds `Cap`. `Text` holds at most `Cap` lines.
  - `type TraceOpts struct{ Services []knowledge.Service; Pattern, Since string; Previous bool; Context int; Cap int }`; `func Trace(ctx, r, t, exportDir string, o TraceOpts) (LogsResult, error)` runs `Logs` per service concurrently (max 4) with `AllPods: true, AllContainers: true`. If ≥80% of matched lines start with an RFC3339 or `yyyy-mm-dd hh:mm:ss[.fff]` timestamp, the merged result is sorted by timestamp; otherwise it is grouped by service. Always exported.
  - `type ServiceSummary struct{ Kind, Name, Image string; Replicas, Ready int; Rollout string; Env []EnvVar; ConfigMaps []string; Events []string; Resources, Probes map[string]any }`; `type EnvVar struct{ Name, Value, Source string /* literal|configmap:<name>/<key>|secret:<name>/<key>|field:<path>|envFrom:configmap:<name>|envFrom:secret:<name> */ }`.
  - `func Describe(ctx, r, t, svc knowledge.Service) (ServiceSummary, error)`. ConfigMap values are resolved with `get configmap <name> -o json`. Secret refs never get a `Value`. Events come from `get events --field-selector involvedObject.name=<name>`. Rollout comes from `rollout status <kind>/<name> --timeout=1s`.

- [ ] **Step 1: Write failing tests** with a fake kubectl that answers by matching argv (testutil.FakeBin with a case statement over `"$*"`):
  - `TestLogsAllPodsPrefixesAndPrevious`: two pods, previous logs requested; lines prefixed, `(previous)` marked, argv for every call contains `--kubeconfig` and `-n`.
  - `TestLogsGrepContext`: `Grep: "233244"`, `Context: 2` → expected group text with `--` separators.
  - `TestLogsCapAndExport`: 1000 lines, Cap 400 → `Truncated`, `Lines == 1000`, export file has 1000 lines, `Text` has 400.
  - `TestLogsStreamsLargeOutput`: 200k lines → same assertions. Peak `Text` size ≤ 400 lines (assert `len(strings.Split(Text,"\n")) <= 401`).
  - `TestTraceSortsByTimestamp` and `TestTraceGroupsWithoutTimestamps`.
  - `TestDescribeEnvSources`: a deployment JSON fixture with literal, configMapKeyRef, secretKeyRef, fieldRef, envFrom configMap and secret → sources as in Interfaces. The configmap value is resolved and the secret value is empty.
- [ ] **Step 2: Run** `go test ./internal/ops/`. Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces.
- [ ] **Step 4: Run tests.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): logs, trace and describe operations`

### Task 7: MCP server with read tools and profile scoping (`internal/mcpserver`, `ctx-core mcp`)

**Files:**
- Create: `internal/mcpserver/server.go`, `scope.go`, `tools_read.go`, `wrap.go`, and tests
- Modify: `cmd/ctx-core/main.go` (`mcp [--profile NAME]`)

**Interfaces:**
- Consumes: everything above.
- Produces:
  - `type Deps struct{ ProfilesDir string; Getter secret.Getter; Kubectl string; Now func() time.Time }`
  - `func New(d Deps, opts Options) *mcp.Server` where `Options{ Profile string /* --profile */; Cwd string }`.
  - Scope resolution, `scope.go`: `func resolveScope(ctx, session, d, opts) (*Scope, error)`, done once per session after initialize. If `--profile` is set, use it. Otherwise, if the client supports roots, call `ListRoots` and resolve every `file://` root with `resolve.Path`. All roots that match must agree (different profiles → no scope, reason `folders belong to more than one client: a, b`). With no roots, resolve `Cwd`. `Scope{Profile *profile.Profile; Store *knowledge.Store; LoadErr error; Redact *secret.Redactor; Activity *knowledge.Activity}`.
  - Tools registered with `mcp.AddTool` and annotations (`ReadOnlyHint: true` for read tools). Input structs use `json`/`jsonschema` tags; outputs are text content. Read tools and their inputs:
    - `client_context{}` → profile name, work dir, envs (name, cluster, namespace, aliases, protected), services (name, kind, aliases), recipe index (name + description), network tool. With no scope, a one-line explanation. With `LoadErr`, its file:line message.
    - `login{env}`; `logs{env, service, pod?, all_pods?, previous?, container?, all_containers?, since?, tail?, grep?, context?, export?}`; `trace{env, services[], pattern, since?, previous?, context?}`; `describe_service{env, service}`; `kubectl_read{env, args[]}` (rejects when `!kube.IsReadOnly(args)` with `use kubectl_change`; output through `RedactSecretObjects` when args include `secret`/`secrets`); `discover_services{env}` (deployments + statefulsets names, marking known aliases); `search_knowledge{query}`; `get_recipe{name}`; `activity{env?, tool?, since?, limit?}`.
  - `wrap.go`: `func (s *Scope) call(tool string, args any, fn func() (string, [][]string, error)) (*mcp.CallToolResult, error)`. Every tool runs through this. It redacts the text and error, maps auth errors, appends an `ActivityEntry` (args redacted, argv recorded, duration, result), and returns errors as tool results with `IsError: true`, never as protocol errors.
  - When the scope is nil or has `LoadErr`, every tool except `client_context` returns the same explanation.

- [ ] **Step 1: Write failing tests** using `mcp.NewInMemoryTransports()` and an `mcp.Client`:
  - `TestNoProfileOnlyContext`: Cwd outside every WORK_DIR → `client_context` explains no client is active; `logs` returns IsError with the same explanation. `~/.ctx/config` containing `active=acme` is ignored.
  - `TestScopeFromRoots` (client advertises roots inside acme's WORK_DIR, Cwd elsewhere → acme) and `TestScopeConflictingRoots` (roots in acme and globex → no scope, message names both).
  - `TestClientContextListsKnowledge` against the spec example store.
  - `TestServerReportsBrokenKnowledge`: broken `services.yaml` → `client_context` text contains `services.yaml:` and a line number; `logs` IsError with same.
  - `TestLogsToolScopedArgv`: fake kubectl; `logs{env:"pp", service:"subs"}`; every recorded argv has `--kubeconfig <CTX_DIR>/kube/acme/nonprod.yaml` and `-n madapi-preprod`.
  - `TestToolOutputRedacted`: fake kubectl prints a stored secret value (fake `ctx secret get`) → result shows `<redacted>`, activity.jsonl does not contain the value.
  - `TestKubectlReadRejectsChange`: `args:["delete","pod","x"]` → IsError mentioning `kubectl_change`, and no kubectl call recorded.
  - `TestOtherProfileInvisible`: two profiles with knowledge; scope acme → `search_knowledge` never returns globex files; `logs{env:"globex-only-env"}` → unknown env listing only acme envs.
  - `TestActivityRecorded`: after a `logs` call, `activity{}` returns one entry with tool `logs`, env `preprod`, result `ok`.
  - `TestMcpCommandStdio` in `cmd/ctx-core`: start `ctx-core mcp --profile acme` as a subprocess (built in TestMain), send initialize + `tools/list` over stdio, and assert `client_context` and `logs` are listed.
- [ ] **Step 2: Run** `go test ./internal/mcpserver/ ./cmd/...`. Expected: FAIL.
- [ ] **Step 3: Implement.** Tool descriptions are one or two plain sentences each. `client_context`'s description says to call it first.
- [ ] **Step 4: Run tests.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(core): ctx mcp server with scoped read tools`

### Task 8: Change tools behind ctx's own confirmation

**Files:**
- Create: `internal/mcpserver/confirm.go`, `tools_change.go`, `portforward.go`, and tests

**Interfaces:**
- Consumes: Task 7 `Scope.call`, Task 5 runner, Task 4 save functions.
- Produces:
  - `type Plan struct{ Tool, Profile, Env, Cluster, Namespace string; Commands [][]string; Writes []string /* file paths for save_* */; Summary string }`
  - `func confirm(ctx context.Context, ss *mcp.ServerSession, p Plan) (ok bool, notRun string, err error)`. If the client did not declare the elicitation capability at initialize → `ok=false`, `notRun` = `ctx needs to ask you before running this, but this client can't show ctx's confirmation. Run it yourself:\n<shell-quoted commands>` (for saves, the file paths and content summary). Otherwise send `Elicit` with a message built from the Plan (profile, env, cluster, namespace, each command shell-quoted, redacted) and a schema of a single boolean `confirm`. Only `Action == "accept"` with `confirm == true` returns ok. Decline, cancel, error or a 120 s timeout → `notRun` = `Not run: you declined` / `Not run: no answer`.
  - Change tools (annotations `DestructiveHint: true`, `ReadOnlyHint: false`). Each resolves the env with `Store.EnvForChange`, builds a Plan, calls `confirm`, and only then executes. All go through `Scope.call`, with activity result `declined` or `not_run` when not executed:
    - `restart{env, service}` → `rollout restart <kind>/<name>`, then `rollout status <kind>/<name> --timeout=300s`. Returns both outputs.
    - `network_test{env, script? , inline?}` (exactly one) → pod from `get pods -l <selector> -o jsonpath={.items[0].metadata.name}` in the network tool namespace (env override, else store default). Then `exec -i <pod> -c <container> -- /bin/sh -s` with the script on stdin, in that namespace. The Plan shows the script name and first 20 lines.
    - `port_forward{action: start|stop|list, env?, target?, ports?, id?}` → start runs `port-forward <target> <ports>` in the background, owned by the server, and returns an id. stop/list need no confirmation. All port-forwards are killed when the session closes.
    - `kubectl_change{env, args[]}` → any args; the Plan shows them.
    - `save_recipe{name, description, tags?, params?, changes_state?, body}`, `save_service{name, kind?, aliases[]}`, `save_script{name, body}` → Plan `Writes` = target path; on accept, the save function from Task 4 runs.

- [ ] **Step 1: Write failing tests** (in-memory client with an `ElicitationHandler` that records the request and returns a scripted answer):
  - `TestRestartAcceptedRuns`: answer accept+true → kubectl argv has `rollout restart deployment/subscription-cis-system` and then `rollout status`, both scoped. The elicitation message contains `preprod`, `madapi-preprod` and the command.
  - `TestRestartDeclinedDoesNothing`, `TestRestartCancelledDoesNothing`, and `TestRestartTimeout` (inject a short timeout): no kubectl call; results `Not run: …`; activity results `declined` / `not_run`.
  - `TestNoElicitationNeverRuns`: client without elicitation capability → no kubectl call; result contains `Run it yourself` and the quoted command.
  - `TestProtectedEnvNeedsCanonicalName`: `restart{env:"<prod alias>"}` → IsError, no elicitation sent; `env:"prod"` → elicitation sent.
  - `TestNetworkTestPipesScript`: fake kubectl records stdin; the script content arrives on stdin of the `exec -i … -- /bin/sh -s` call in namespace `network-utility`.
  - `TestPortForwardLifecycle`: start (accepted) → list shows it → session close kills the process (fake kubectl sleeps; assert it's gone).
  - `TestSaveRecipeNeedsConfirm`: declined → file absent; accepted → file present and returned by `get_recipe`.
- [ ] **Step 2: Run** `go test ./internal/mcpserver/`. Expected: FAIL.
- [ ] **Step 3: Verify the SDK and host:** check that the go-sdk version in `go.mod` exposes server-side `Elicit` and the client elicitation capability. Check that the installed Claude Code shows MCP elicitation requests (start `ctx-core mcp` registered in Claude Code, call `restart` against a fake kubectl on PATH, and confirm the dialog appears). **If Claude Code does not support elicitation, stop and report back; do not substitute another mechanism** (spec decision).
- [ ] **Step 4: Implement** per Interfaces.
- [ ] **Step 5: Run tests.** Expected: PASS.
- [ ] **Step 6: Commit** `feat(core): change tools gated by ctx confirmation`

### Task 9: Install, register, doctor, docs, release

**Files:**
- Create: `internal/mcpserver/install.go` + test (or `cmd/ctx-core/install.go`)
- Modify: `cmd/ctx-core/main.go` (`mcp install claude|cursor`), `install.sh`, `lib/cmd_ops.sh` (`cmd_doctor`), `.github/workflows/release.yml`, `packaging/homebrew-tap/Formula/ctx.rb`, `README.md`, `CHANGELOG.md`, `docs/sprint/SPRINT.md`, `AGENTS.md` (Go commands + where things live), `test/test.sh`

**Interfaces:**
- Produces:
  - `ctx mcp install claude` → runs `claude mcp add --scope user ctx -- ctx mcp`. If `claude` isn't on PATH, prints that command for the user and exits 1.
  - `ctx mcp install cursor` → merges `{"mcpServers":{"ctx":{"command":"ctx","args":["mcp"]}}}` into `~/.cursor/mcp.json`, keeping other servers. A backup is written first to `~/.cursor/mcp.json.ctx-backup-<timestamp>`. Running it twice changes nothing.
  - `install.sh`: from a checkout with `go` available → `go build -ldflags "-X main.version=$CTX_VERSION" -o "$stage_lib/ctx-core" ./cmd/ctx-core`. Remote installs download `ctx-core_<os>_<arch>` from the GitHub release matching `v$CTX_VERSION`. If the release or asset is missing, it warns `ctx mcp unavailable: no ctx-core build for <os>/<arch>` and continues (exit 0). It is installed next to the libs with mode 0755 and goes through the same staged swap and `ctx-core version` sanity check.
  - `release.yml`: builds `ctx-core_{darwin,linux}_{amd64,arm64}` with the version ldflag from the tag and attaches them plus `checksums.txt`. The installer verifies the checksum when `shasum`/`sha256sum` is available.
  - `ctx doctor`: line `ctx-core: <version> (<path>)`, or a warning `ctx-core missing — ctx mcp unavailable`.
  - Formula: `depends_on "go" => :build`, builds `ctx-core` into `lib`.

- [ ] **Step 1: Write failing tests:** `TestInstallCursorMerges` (an existing file with another server keeps it, adds ctx, writes a backup, idempotent on second run). `TestInstallClaudeMissing` (empty PATH → exit 1, prints the `claude mcp add` command). `test_install_builds_ctx_core` in test/test.sh: `CTX_INSTALL_BIN/LIB` temp, `CTX_UPGRADE_ONLY=1 bash install.sh` from the checkout → `$lib/ctx-core version` prints the version (skip when `go` is missing). `test_doctor_reports_ctx_core`.
- [ ] **Step 2: Run** `go test ./...` and `bash test/test.sh`. Expected: FAIL.
- [ ] **Step 3: Implement** per Interfaces.
- [ ] **Step 4: Docs.** README gets a section "AI ops agent (`ctx mcp`)": what it is, `ctx knowledge init`, the three YAML files (use the spec's MTN example with hostnames replaced by `example.net`), `ctx secret set <profile> <KEY>` for login secrets, `ctx mcp install claude`, confirmation behavior, and what is never shown (secrets). Add a CHANGELOG [Unreleased] → Added entry. Add SPRINT.md row `B8 ctx mcp ops agent`. Add `go test ./...` to AGENTS.md commands and the Go packages to its table.
- [ ] **Step 5: Run** `go test ./...`, `bash test/test.sh`, shellcheck. Expected: all PASS.
- [ ] **Step 6: Commit** `feat: install and register ctx mcp; docs`

### Task 10: Manual acceptance on the maintainer's machine

Not automatable; the executor prepares and the maintainer runs it.

- [ ] **Step 1:** Upgrade local Go (1.21.3 is installed), then `bash install.sh` from the branch and run `ctx doctor`. Expected: a `ctx-core` line with the version.
- [ ] **Step 2:** The maintainer runs `ctx secret set mtn TKGI_PASSWORD` and removes the plain-text value and `pbcopy` from the `tkgi_*` aliases in `~/.zshrc`.
- [ ] **Step 3:** `ctx knowledge init mtn`, then fill the files from the spec example with the real hosts (nonprod/prod/sandbox clusters; dev/preprod/prod envs with `madapi-*` namespaces; prod `protected: true`; network tool `network-utility` / `app=network-tool` / `network-tool`). Make the tkgi steps non-interactive (check how `tkgi get-credentials` accepts the password on this machine). Copy the existing network scripts into `scripts/`.
- [ ] **Step 4:** `ctx mcp install claude`. In Claude Code under the MTN folder, run spec acceptance items 3–6 and record the outcome of each in the PR description.

---

## Self-review notes

- Spec coverage: scoping/fail-closed (T7), knowledge layout and init (T4), login types and isolation (T5), every tool in both tables (T6–T8), confirmation and its fallback (T8), redaction and activity (T5, T7), protected envs (T4, T8), Go foundations and conformance (T2, T3), build/CI/release/install (T1, T9), testing with fakes (T5–T8), acceptance (T10).
- Out of scope per spec: the `ctx use` export fix (handled separately), per-terminal cloud CLIs, porting other commands.
