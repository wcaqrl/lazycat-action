# lazycat-action

[English](README.md)

`lazycat-action` 是平台无关的懒猫官方应用商店发布工具。它可以定时发现 GitHub、Gitee 或 OCI 镜像的新 revision，按需构建定制镜像，调用懒猫官方镜像转存接口，生成并校验 LPK，最后提交官方开发者平台审核。

源项目不需要包含懒猫文件。`package.yml`、`lzc-build.yml`、`lzc-manifest.yml`、Dockerfile、图标、截图和发布配方应保存在单独的应用适配项目中。本仓库只包含通用执行器、平台入口和中性测试数据。

发布目标只有懒猫官方应用商店。本项目不包含私有商店客户端、配置或凭据。

## 三个仓库一起使用

[Poster](https://github.com/wcaqrl/poster) 是一个具体的上游示例：它在 `vX.Y.Z` 标签发布时构建公开的 GHCR 镜像。对应的 [poster-adapter](https://github.com/wcaqrl/poster-adapter) 单独保存懒猫的 `package.yml`、`lzc-build.yml`、`lzc-manifest.yml`、图标与截图，以及 `version: 2` 的 `lazycat-action.yml`。本仓库只提供通用工具和可复用工作流，不收录具体应用。

适配仓库的定时工作流调用 `wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1`；`source.kind: oci` 加 `select.strategy: semver-tag` 从 GHCR 找到目标架构最高稳定版本及不可变镜像摘要。首次接入需要在适配仓库的 Actions Secrets 配置 `LZC_API_TOKEN`（开发者 PAT），并给予 `GITHUB_TOKEN` 写入仓库的权限。先手动执行 `dry-run` 检查选中的摘要，再执行正式任务。正式运行会转存到懒猫官方仓库、构建并检查 LPK、将版本与锁文件提交回适配仓库，最后提交官方应用商店审核。完成新版发布前，先检查工具仓库的 `v1.3.0` Release 与浮动 `v1` 标签均已生效。

## 更新判断

每个应用选择一个权威来源：

| 来源 | 选择方式 | 不可变标识 |
|---|---|---|
| OCI 镜像 | 稳定 SemVer Tag | 目标平台 image digest |
| Git Tag/Release Tag | 最高稳定 SemVer Tag | Tag 对应的 commit SHA |
| Git 分支 | 指定分支或远程默认分支 | 分支 HEAD commit SHA |

最终指纹还包含 `build.prepare` 命令、Dockerfile、构建参数和构建上下文内容。上游不变但插件列表或 Dockerfile 改变时，仍会生成新版本。

状态保存在 `.lazycat-action.lock.yml`：

- `packaged`：镜像和 LPK 已生成，可以重试官方提交。
- `submitted`：该指纹已经成功提交或确认线上已有相同版本，再次检查为 no-op。

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
  url: git@gitee.com:example/private-app.git
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
    changelog_locales: [zh, en]
```

`branch: auto` 通过 `git ls-remote --symref <url> HEAD` 读取真实默认分支，不假设 GitHub 一定使用 `main`，也不假设 Gitee 一定使用 `master`。分支 commit 变化时，应用版本自动增加 patch。

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

`command` 模式还会收到 `LAZYCAT_SOURCE_DIR`、`LAZYCAT_OUTPUT_IMAGE`、`LAZYCAT_BUILD_FINGERPRINT` 和 `LAZYCAT_TARGET_PLATFORM`。Git revision 会被 checkout 到临时目录，任务结束后删除。

## 私有 GitHub/Gitee 仓库

配置只保存逻辑凭据名称：

```yaml
source:
  kind: git
  url: git@gitee.com:example/private-app.git
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

```yaml
name: Check and publish LazyCat application

on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch:

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      config: lazycat-action.yml
      operation: auto
      toolchains: docker
    secrets: inherit
```

常用 Secret：

- `LZC_API_TOKEN`：懒猫官方开发者 PAT。
- `SOURCE_SSH_KEY`、`SOURCE_KNOWN_HOSTS`：私有 Git 源。
- `REGISTRY`、`REGISTRY_USERNAME`、`REGISTRY_PASSWORD`：定制镜像中转仓库。

Reusable Workflow 使用本地校验过的 LPK 和 SHA256 直接提交官方平台，不要求创建 GitHub Release。

## Gitee Go、自建 Runner 和本地 CLI

CLI 不依赖 GitHub 事件：

```bash
lazycat-action run --operation check --config lazycat-action.yml
```

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

Gitee Go 可以调用 `scripts/gitee-run.sh`；该入口默认在发现更新并生成 LPK 后提交官方审核，详见 [Gitee Go 文档](docs/gitee-go.zh-CN.md)。Runner 所在平台和源仓库平台互不绑定。

## Version 1 兼容

已有 `version: 1` OCI 镜像配置继续受支持。Version 2 用于远程 Git 发现、构建配方指纹和可恢复状态。新项目应使用 Version 2。

## 开发

```bash
go test ./...
go vet ./...
```

Action 发布包包含 Linux amd64 和 arm64 二进制及 SHA256 校验文件。
