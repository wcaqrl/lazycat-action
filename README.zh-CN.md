# lazycat-action

[English](README.md)

`lazycat-action` 用于通过 GitHub 应用适配仓库自动发布懒猫官方应用商店。它由 GitHub Actions 定时检查上游 GitHub 版本或官方 OCI 镜像版本，按需构建定制镜像，调用官方镜像转存接口，生成并校验 LPK，最后提交开发者平台审核。

源项目不需要包含懒猫文件。`package.yml`、`lzc-build.yml`、`lzc-manifest.yml`、Dockerfile、图标、截图和发布配方应保存在单独的应用适配项目中。本仓库只包含通用执行器、平台入口和中性测试数据。

发布目标只有懒猫官方应用商店。本项目不包含私有商店客户端、配置或凭据。

当前入门文档、AI 指南和模板只围绕 **GitHub 仓库 + GitHub Actions** 展开。镜像来源仍可使用 GHCR、Docker Hub 等 OCI 仓库。

## 从这里开始

- **开发者第一次接入：**[从空仓库创建适配器](docs/quickstart.zh-CN.md)。
- **交给自己的 AI：**[可直接复制的任务说明](docs/ai-adapter-guide.md)和 [AI Skill](skills/lazycat-github-action/SKILL.md)。
- **选择构建方式：**[单镜像、多镜像、自定义镜像和源码构建](skills/lazycat-github-action/references/scenarios.zh-CN.md)。
- **直接复制文件：**[完整适配仓库模板](skills/lazycat-github-action/assets/adapter/)，包含隐藏工作流、包配置、占位图标及忽略规则。

模板的手动运行默认 `dry-run`；定时任务也先只检查，设置适配仓库 Actions 变量 `LAZYCAT_AUTO_PUBLISH=true` 后才自动送审。这是新模板的控制方式，不改变已有适配器或执行器的默认行为。

## 三个仓库一起使用

上游仓库提供源码、正式版本及可能存在的官方镜像；你自己的 `xxx-adapter` 保存应用运行配置、测试脚本、版本选择规则和定时工作流；本仓库提供通用执行器。源项目无需加入懒猫配置，也不需要 fork 执行器。

适配仓库决定“监控谁、什么时候执行、如何定制和测试”，`lazycat-action` 负责按配置发现和比较版本、固定镜像、转存、打包、记录状态及送审。任务运行在适配仓库的 GitHub Runner 中，不是本工具后台常驻扫描所有应用。

适配器调用 `wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1`。PAT 和源仓库/镜像仓库凭据配置在适配仓库的 Secrets 中；第一次先执行 `dry-run`，再通过实际应用测试后正式发布。

## 更新判断

每个应用选择一个权威来源：

| 来源 | 选择方式 | 不可变标识 |
|---|---|---|
| OCI 镜像 | 稳定 SemVer Tag | 目标平台 image digest |
| Git Tag/Release Tag | 最高稳定 SemVer Tag | Tag 对应的 commit SHA |
| Git 分支 | 指定分支或远程默认分支 | 分支 HEAD commit SHA |

当官方商店已有待审核版本时，Action 仍会检查上游：候选版本更高则继续构建和提交，由官方商店在接收新审核后自动取消旧审核；候选版本相同或更低时暂停。同一个已记录 Git Tag 如果被移动到另一个 commit，流程会直接报错，不会用相同版本号静默发布不同源码。

最终指纹还包含 `build.prepare` 命令、Dockerfile、构建参数和构建上下文内容。送审历史同时保留不可变的源标识，已经送审的源版本不会因配方调整被定时任务重复提交。

状态保存在 `.lazycat-action.lock.yml`：

- `packaged`：镜像和 LPK 已生成，可以重试官方提交。
- `submitted`：该指纹已经成功提交或确认线上已有相同版本，再次检查为 no-op。

锁文件格式升级为 `version: 2`，自动兼容原有格式。`packaged_at` 在送审之前提交到仓库；`reviews` 保存每次提交的源版本、revision、应用版本、LPK 摘要、审核 ID、创建时间、状态和拒绝原因。每次正式检查同步仍在等待的审核。已拒绝、通过、取消或删除的源版本都会跳过自动重复送审；新的源版本仍可替换旧待审核版本。

审核列表通过 `created_at_start` 和 `created_at_end` 成对查询（上海时间），固定围绕审核创建时间，再在客户端匹配审核 ID。恢复“送审成功但状态锁推送失败”时，按已提交的 `packaged_at` 限定时间范围，匹配应用版本、提交渠道和 LPK 摘要。不会把上次轮询时间作为创建时间起点。旧锁没有时间戳时，仅在首次迁移按 `sort=-id` 分页寻找记录；找不到历史审核时记录为 `untracked`，继续阻止重复送审，避免每天遍历全部历史。

### 下载 LPK 与手动修复审核拒绝

可复用工作流打包完成后，`submit-official` 和 `publish-github-release` 并行运行。Release 在适配仓库创建 `v<应用版本>` Tag，指向打包提交，附件只有 `<包名>-v<应用版本>.lpk`。Release Job 失败显示警告，不阻止商店送审；已有 Tag 或附件内容冲突时不会覆盖。内部仍校验 SHA256，不发布 `.sha256` 文件。

Adapter 的 `workflow_dispatch` 增加 `retry-rejected` 布尔输入并传给可复用工作流。默认关闭。修复适配配置后，手动关闭 `dry-run` 并打开 `retry-rejected`，Action 使用同一被拒绝的源 revision，将应用版本提升一个 patch 后重新送审。旧拒绝记录保留。这个参数不适用于未被拒绝的源版本。

CLI 对应 `run --retry-rejected --publish-after-check`。各 Adapter 继续引用 `@v1`，需要 `contents: write` 和仓库 Secret `LZC_API_TOKEN`。审核状态同步仅适用于配置 `version: 2` 的源流程。

### 自动抓取本次更新日志

当版本来源是 OCI 镜像时，在**适配仓库**的 `lazycat-action.yml` 中指定构建该镜像的 Git 仓库：

```yaml
changelog:
  git_url: https://github.com/example/application.git
  max_commits: 20
```

默认从 Git 读取“当前应用版本的标签 → 新镜像标签”之间的提交标题作为更新日志，`v1.2.3` 和 `1.2.3` 标签均可匹配。Git 源也可以配置相同地址；私有仓库可写 `auth_ref: source`，沿用 `SOURCE_SSH_KEY` 和 `SOURCE_KNOWN_HOSTS` 或 `SOURCE_TOKEN`，无需在 YAML 放密钥。当前应用版本没有上游标签时只列出目标版本最新一条提交；目标版本标签不存在则在转存、送审前报错，避免提交错误说明。

GitHub 项目可以直接使用目标版本的 Release 正文，避免把版本之间的全部提交当作本次更新日志：

```yaml
changelog:
  mode: github-release
  git_url: https://github.com/example/application.git
```

此模式要求选中的稳定标签已经发布对应 GitHub Release。Release 不存在、仍是草稿或预发布时，流程会在镜像转存之前停止。

只读 `dry-run` 的结果会显示 `changelog`，正式运行会把它写入锁文件供失败后重试。工作流的 `changelog` 输入或 CLI 的 `--changelog` 可人工覆盖；没有配置 `changelog.git_url` 的适配项目仍使用旧的通用版本说明。

正式送审时在本地判断更新日志的原文语种，只提交一个字段：中文填写 `version.changelogs.zh`，英文填写 `version.changelogs.en`。这是中英文轻量判断，混合文本按主体语种归类，排除固定 `Upstream` 标题、链接、代码和技术标识的干扰。原文不会被翻译或改写，缺少语种由应用商店服务端补翻；Action 不调用翻译接口，也不需要 TokenHub 密钥。

`changelog_locales` 是可选的历史兼容配置，不再表示要填写的目标语种列表。旧的 `[zh, en]` 配置仍能读取，但不会把同一原文复制成双语。只有文本无法判断语种时（例如纯数字、符号），单一 `[zh]` 或 `[en]` 可作为提示；否则在上传 LPK 之前明确报错。暂不自动识别其它语种，也不改写历史审核日志。

## Version 2 配置

### 没有 Tag 的 Git 分支

```yaml
version: 2

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/application.lpk
  target_arch: amd64

source:
  kind: git
  url: git@github.com:example/private-app.git
  auth_ref: source
  select:
    strategy: branch-head
    branch: auto

state:
  file: .lazycat-action.lock.yml

update:
  strategy: publish
  allow_downgrade: false

build:
  run_buildscript: true
  prepare:
    mode: command
    context: scripts
    command: ./scripts/build-source.sh

stores:
  official:
    enabled: true
    skip_if_version_exists: true
    create_if_missing: false
```

`branch: auto` 通过 `git ls-remote --symref <url> HEAD` 读取 GitHub 仓库的真实默认分支，不假设一定使用 `main`。分支 commit 变化时，应用版本自动增加 patch。

### Git SemVer Tag

```yaml
source:
  kind: git
  url: https://github.com/example/application.git
  select:
    strategy: semver-tag
    tag_regex: '^v?[0-9]+\.[0-9]+\.[0-9]+$'
```

### OCI 原样转存

```yaml
source:
  kind: oci
  image: docker.io/example/application
  select:
    strategy: semver-tag
    channel: stable
    tag_regex: '^v?[0-9]+\.[0-9]+\.[0-9]+$'

build:
  prepare:
    mode: passthrough

images:
  - id: runtime
    target: service
    service: application
    delivery:
      mode: lazycat
```

### 基于上游的定制镜像

```yaml
source:
  kind: oci
  image: docker.io/example/application
  select:
    strategy: semver-tag
    tag_regex: '^[0-9]+\.[0-9]+\.[0-9]+$'

build:
  toolchains:
    - kind: docker
  prepare:
    mode: dockerfile
    context: image
    dockerfile: image/Dockerfile
    output_image: ghcr.io/example/application-lazycat:{version}-{fingerprint}
    build_args:
      FEATURE_SET: production
```

Docker 构建自动收到：

```text
SOURCE_IMAGE=<tag>@<digest>
SOURCE_REF=<tag or git ref>
SOURCE_REVISION=<digest or commit SHA>
SOURCE_VERSION=<normalized version>
```

Dockerfile 应通过 `ARG SOURCE_IMAGE` 和 `FROM ${SOURCE_IMAGE}` 固定基础镜像。构建结果必须推送到懒猫开发者平台能够读取的 OCI Registry。

### 一个版本协调多个运行镜像

上游 Git 标签同时发布多个容器时，使用 `build.prepare.mode: images`，并在每个 `images[].source` 中引用同一个来源标签：

```yaml
source:
  kind: git
  url: https://github.com/example/application.git
  select:
    strategy: semver-tag
    tag_regex: '^v[0-9]+\.[0-9]+\.[0-9]+$'

build:
  prepare:
    mode: images

images:
  - id: server
    target: service
    service: server
    source: ghcr.io/example/server:{tag}
    delivery:
      mode: lazycat
  - id: worker
    target: service
    service: worker
    source: ghcr.io/example/worker:{tag}
    delivery:
      mode: lazycat
  - id: database
    target: service
    service: database
    source: ghcr.io/example/database:14@sha256:...
    delivery:
      mode: lazycat
```

支持 `{tag}`、`{source_version}`、`{version}` 和 `{revision}` 占位符。流程先固定并交付全部镜像，全部成功后才一次性修改 Manifest；构建或状态写入失败时会恢复原来的镜像和应用版本。锁文件会保存整组源摘要及官方运行地址。

如果官方 copy-image 服务无法稳定读取源 Registry，可在 `delivery` 下配置 `staging_image`，例如 `ttl.sh/example-{id}-{version}-{revision}:24h`。Action 会用固定摘要通过 `crane` 中转，校验中转摘要与源摘要一致，再提交官方转存。复用工作流同时设置 `enable-image-staging: true`。

如果已有无需登录的可信代理，可使用 `copy_source`，例如 `ghcr.nju.edu.cn/example/server:{tag}`。Action 会同时检查原始源和代理的目标平台摘要，只有两者完全一致才从代理转存。`copy_source` 与 `staging_image` 不能同时使用。

`command` 模式还会收到 `LAZYCAT_SOURCE_DIR`、`LAZYCAT_OUTPUT_IMAGE`、`LAZYCAT_BUILD_FINGERPRINT` 和 `LAZYCAT_TARGET_PLATFORM`。Git revision 会被 checkout 到临时目录，任务结束后删除。

## 私有 GitHub 仓库

配置只保存逻辑凭据名称：

```yaml
source:
  kind: git
  url: git@github.com:example/private-app.git
  auth_ref: source
```

对应环境变量：

```text
LAZYCAT_AUTH_SOURCE_SSH_KEY
LAZYCAT_AUTH_SOURCE_KNOWN_HOSTS
```

HTTPS Token 使用：

```text
LAZYCAT_AUTH_SOURCE_TOKEN
LAZYCAT_AUTH_SOURCE_USERNAME
```

SSH Key 写入权限为 `0600` 的临时文件；Token 通过 `GIT_ASKPASS` 提供。仓库 URL、配置和日志中都不写凭据。

## GitHub Actions

使用[完整工作流模板](skills/lazycat-github-action/assets/adapter/.github/workflows/update.yml)，包含只读 push、手动 `dry-run`/`retry-rejected`、定时发布开关和写回权限。更多参数见[工作流说明](skills/lazycat-github-action/references/workflows.md)。

常用 Secret：

- `LZC_API_TOKEN`：懒猫官方开发者 PAT。
- `SOURCE_SSH_KEY`、`SOURCE_KNOWN_HOSTS`：私有 Git 源。
- `REGISTRY`、`REGISTRY_USERNAME`、`REGISTRY_PASSWORD`：定制镜像中转仓库。

Reusable Workflow 使用本地校验过的 LPK 和 SHA256 直接提交官方平台，不要求创建 GitHub Release。

## 本地 CLI 调试

GitHub Actions 是主要运行入口；需要本地调试时，也可以显式调用 CLI：

```bash
lazycat-action run --operation check --config lazycat-action.yml --dry-run
```

不带 `--dry-run` 的 `check` 会在发现更新时转存/构建，并非只读命令。检查完成后是否自动送审由调用入口决定。

官方发布：

```bash
lazycat-action run \
  --operation publish-official \
  --config lazycat-action.yml \
  --version 1.2.3 \
  --lpk-path dist/application.lpk \
  --sha256 <sha256> \
  --changelog "Release 1.2.3"
```

## Version 1 兼容

已有 `version: 1` OCI 镜像配置继续受支持。Version 2 用于远程 Git 发现、构建配方指纹和可恢复状态。新项目应使用 Version 2。

## 开发

```bash
go test ./...
go vet ./...
```

Action 发布包包含 Linux amd64 和 arm64 二进制及 SHA256 校验文件。
