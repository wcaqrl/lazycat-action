# LazyCat GitHub Action

[English](README.md)

本仓库基于 MIT 许可的
[`ca-x/lazycat-github-action`](https://github.com/ca-x/lazycat-github-action)
独立维护，原项目与派生代码的版权声明均保留在 `LICENSE` 中。

`wcaqrl/lazycat-action` 用于检查 Docker 镜像版本、精确更新 LazyCat Manifest、构建 LPK、创建更新 Pull Request，并把校验后的 LPK 上传到 GitHub Release。

Action 使用 [`github.com/lib-x/lzc-toolkit-go`](https://github.com/lib-x/lzc-toolkit-go) `v0.6.0`，兼容基线是 `@lazycatcloud/lzc-cli` `2.0.9`。

当前交付范围：

- Milestone 1：静态 Web 和 Exec 构建、LPK 校验、SHA256、amd64 和 arm64 Action 二进制。
- Milestone 2：stable、beta、date、nightly 和 custom OCI 检查；LazyCat、direct 和 mirror 镜像交付；Pull Request；Artifact；tag；Release；Release Asset。
- Milestone 3：懒猫官方开发者平台提交、喵喵私有商店提交、完整源码构建示例和仓库内 Agent Skill。

## 集中管理应用

本仓库可以集中维护懒猫应用的完整打包层，源项目无需知道自己会被制作成懒猫应用。每个应用的 `package.yml`、构建配置、Manifest、图标、镜像监控规则和商店资料统一放在 `targets/<应用>/`。本仓库的工作流定期检查上游公开 OCI 镜像；发现新版本后，将镜像转存至懒猫官方仓库，更新受管文件，构建并校验 LPK，最后提交审核。

首个受管目标是 [`targets/poster`](targets/poster)。它的上游项目只负责发布 `ghcr.io/wcaqrl/poster`，所有懒猫专用文件和监控工作流都位于本仓库。`LZC_API_TOKEN` 应配置为本仓库的 Actions Secret，无需配置到上游应用仓库。

## 选择使用方式

两个公开入口都受支持，并共同跟随浮动的 `v1` 发布标签：

| 入口 | 引用方式 | 适用场景 |
|---|---|---|
| Composite Action | `wcaqrl/lazycat-action@v1` | 现有 job 已经负责 checkout、权限、工具链安装和 GitHub 写操作。 |
| Reusable Workflow | `wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1` | 需要完整 LazyCat CI/CD，包括工具链、Pull Request、Artifact、tag、Release、Asset 和商店发布。 |

为什么 reusable workflow 的写法比 `KSXGitHub/github-actions-deploy-aur@v4.2.0` 这类 Action 长？因为 GitHub 对两种入口规定了不同语法。仓库根目录的 `action.yml` 属于 step 级 Action，使用较短的 `<owner>/<repo>@<ref>`；reusable workflow 属于 job 级工作流文件，必须写成 `<owner>/<repo>/.github/workflows/<file>@<ref>`。路径较长不代表需要安装另一个仓库，只是明确选择“完整自动化工作流”，而不是单个 composite step。

当前官方 checkout 和 Node setup Action 使用 Node.js 24 运行时；self-hosted GitHub Actions Runner 必须为 `v2.327.1` 或更高版本。由调用方管理的 composite job 应使用 `actions/checkout@v7` 和 `actions/setup-node@v7`；reusable workflow 内部的 setup-go、github-script、Docker setup/login、Pull Request 创建和构建证明 Action 也使用当前受支持的主版本。

一般 CI/CD 推荐调用 reusable workflow：

```yaml
jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      config: .github/lazycat-action.yml
    secrets: inherit
```

也可以在现有 job 中直接调用 composite Action：

```yaml
- uses: wcaqrl/lazycat-action@v1
  id: lazycat
  with:
    operation: build
    version: ${{ github.ref_name }}
```

Gitea Actions 和 Forgejo Actions 用户应通过完整 URL 直接调用 composite Action，GitHub reusable workflow 不能原样迁移。具体配置和限制见[《在 Gitea Actions 和 Forgejo Actions 中使用 LazyCat GitHub Action》](docs/gitea-forgejo-actions.zh-CN.md)。

调用方不需要编译本项目。启动脚本会按 Runner 架构下载 Action 二进制，并校验发布包 SHA256。

## 进度日志

Action 使用 Go 官方 `log/slog` 输出结构化进度，同时不会打印 Secret 值和受保护的构建环境变量。每次运行会先显示执行模式（`docker-image`、`source-build`、`prebuilt-content` 或 `store-publish`），再按实际流程输出以下阶段：

- Docker 版本查询、候选数量、选中的 Tag/版本/digest/平台、镜像交付开始、节流后的 layer 进度和交付结果。
- LPK buildscript 开始、包组装、官方 lint，以及最终 LPK 路径、大小和 SHA256。
- 目标商店、已验证的发布文件、同版本跳过、提交开始和提交结果。

项目 buildscript 的 stdout/stderr 会实时透传，便于定位原生工具缺失等错误。Action 会显示进程退出码，但不会打印 buildscript 正文或受保护的环境变量。

本地 LPK 校验失败时，Action 会在安全范围内给出结构化诊断。例如模板 Manifest 解析失败可显示 `upstream=INVALID_CONFIG`、`op=build.template_manifest`、`path=lzc-manifest.yml` 和限长后的 `message="yaml: line 90: ..."`。路径只有在精确命中项目检查阶段已确认的 package、build 或 Manifest 文件时才会公开；Runner 路径、未知路径、Unicode 日志分隔符和疑似凭据内容都会被隐藏。

## 使用 Skill

可以直接用自然语言要求 Agent，例如：“检查这个 LazyCat 仓库，创建同时发布两个商店的版本化 GitHub Release workflow，并保护 Go Template Manifest。”仓库 Skill 会检查 `package.yml`、`lzc-build.yml`、配置的 Manifest、工具链文件、`.gitignore`、Git 已跟踪的 `*.lpk` 和已有 `.github/` 内容；随后创建或更新 `.github/lazycat-action.yml` 与所需的 `.github/workflows/*.yml`，并报告所有变更文件、验证结果、未决问题和必需的 GitHub Secret 名称，但不会读取 Secret 值。

如果路径、镜像归属、策略、商店或工具链无法从仓库证明，Skill 会在生成项目文件前暂停确认。迁移历史 LPK 时，它先运行 `git ls-files '*.lpk'`，报告已跟踪文件数量和总字节数，并在删除前显示单独、醒目的 STOP。用户拒绝时保留全部文件；批准后只删除清点过的文件，并添加 `*.lpk` 和输出目录 ignore 规则。除非另行提出请求，否则绝不重写 Git 历史或回填旧 Release。

发布 workflow 会显式映射各已启用商店所需的 Secret，不再只依赖 `secrets: inherit`。组织 Secret 必须授权每个新加入的仓库；同名 Secret 的优先级为 Environment > Repository > Organization。

需要带版本号的 Release 文件时，设置 `versioned-release-asset: true`。原始已验证构建输出继续作为 validation Artifact，GitHub Release 使用 `<package-id>-v<version>.lpk`。私有商店接收已验证的 Release Asset URL 和 SHA256；官方商店上传同一份本地已验证 LPK 字节及 SHA256，但不会接收 GitHub Release URL。

Go Template Manifest 永远不会被执行或求值。独立的 `if`、`else`、`end`、`with`、`range` 控制行会连同缩进和 trim marker 被原样保护、恢复，内联表达式保持不变。marker 丢失/冲突、保护后 YAML 无效、目标歧义或模板意外变化时会 fail closed；完成前还会验证控制行和真实构建。

## Runner 架构和 LazyCat 目标架构

Action 的运行机器和 LazyCat 应用目标是两件事：

| 层次 | 支持值 |
|---|---|
| Runner 系统 | Linux |
| Runner CPU | amd64 或 arm64 |
| LazyCat 目标系统 | Linux |
| LazyCat 目标 CPU | 由 `project.target_arch` 配置；默认 amd64，可选 arm64 |
| OCI 检查和复制平台 | 与项目目标一致的 `linux/amd64` 或 `linux/arm64` |

ARM64 self-hosted Runner 使用 ARM64 版本的 Action 二进制，但 buildscript 仍然收到：

```text
LAZYCAT_TARGET_OS=linux
LAZYCAT_TARGET_ARCH=<project.target_arch>
LAZYCAT_TARGET_PLATFORM=linux/<project.target_arch>
```

reusable workflow 支持传入 Linux Runner 标签：

```yaml
jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      runner: self-hosted-linux-arm64
      config: .github/lazycat-action.yml
    secrets: inherit
```

上面的标签只是示例，需要在 self-hosted Runner 上自行配置。切换 Runner 不会把 LPK 目标改成 ARM。

## 基本概念

- `package.yml` 保存 package ID、版本、显示信息和 locales。
- `lzc-manifest.yml` 保存应用路由，以及可选的 application 或 service 镜像。
- `lzc-build.yml` 指向 Manifest、内容目录和可选的项目 `buildscript`。
- `.github/lazycat-action.yml` 告诉 Action 它负责哪个版本来源和哪些镜像目标。
- Workflow Artifact 是 GitHub Actions 保留的 CI 产物。
- Release Asset 是挂在 GitHub Release 下的公开版本文件。

Action 默认执行基础 LPK lint。设置 `stores.official.enabled: true` 后会执行懒猫官方 lint profile，同时要求所有受管运行时镜像使用 `delivery.mode: lazycat`。

## Docker 镜像应用快速开始

假设应用有数据库服务 `db` 和真正对外显示页面的 Web 服务 `web`：

```yaml
# lzc-manifest.yml
application:
  subdomain: example
  routes:
    - /=http://web:8080/

services:
  db:
    # upstream: postgres:17
    image: registry.lazycat.cloud/acme/postgres:copy-id
  web:
    # upstream: ghcr.io/acme/example-web:v1.0.0
    image: registry.lazycat.cloud/acme/example-web:old
```

Action 不会猜测 `web` 是主服务，需要显式配置两个不同职责：

- `update.version_source.image: web` 表示使用 `web` 的镜像版本更新 `package.yml.version`。
- `images[].target: service` 和 `service: web` 表示 Manifest 编辑器只能修改 `services.web.image`。

`db` 已经使用 LazyCat Registry 镜像，但没有出现在 `images` 配置中，因此这套自动化不会修改它。

创建 `.github/lazycat-action.yml`：

```yaml
version: 1

project:
  root: .
  build_config: lzc-build.yml
  package_file: package.yml
  output: dist/example.lpk
  target_arch: amd64

update:
  strategy: pull
  allow_downgrade: false
  version_source:
    type: image
    image: web

build:
  run_buildscript: true

images:
  - id: web
    target: service
    service: web
    source: ghcr.io/acme/example-web
    channel: stable
    delivery:
      mode: lazycat

stores:
  official:
    enabled: true
    create_if_missing: false
    changelog_locales: [zh, en]
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
  private:
    enabled: false
```

`allow_downgrade` 默认为 `false`。版本来源镜像完成标签到 SemVer 的映射后，如果候选版本低于当前 `package.yml.version`，Action 会在复制镜像和修改文件前阻止降级。版本相同仍可刷新镜像引用或 digest。只有明确执行回退时才设置为 `true`。

生产环境默认使用 `appstore.api.lazycat.cloud`，建议把 PAT 保存为 GitHub Secret `LZC_API_TOKEN`。历史 workflow 可以继续把原有 lzc-cli 会话 token 保存在 `LAZYCAT_TOKEN`。需要切换 PAT API 环境时，可用 `LZC_API_HOST` 覆盖默认域名。

再添加定时和手动触发 workflow：

```yaml
name: Check LazyCat images

on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch:

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

`strategy: pull` 是默认策略。发现新镜像后，workflow 只更新显式配置的目标，构建并校验 LPK，上传 Workflow Artifact，然后创建或更新 `lazycat/update-all`。

使用 `operation: auto` 时，`workflow_dispatch` 会构建 Git 版本源项目（未提供版本输入时读取 `package.yml.version`），对镜像版本源项目则执行检查；手动显式提供版本始终执行构建。Tag push 执行构建，schedule 执行镜像检查。其他情况下显式操作仍保持原语义，但 Tag 和 Release 事件会拒绝 `check`，避免把镜像更新发布到无关的事件 Tag 下。

如果只想处理一个镜像，可以传 `image-id`：

```yaml
with:
  operation: check
  image-id: web
  config: .github/lazycat-action.yml
```

使用 `strategy: pull` 时，可以单独选择非版本来源镜像，Manifest 会更新，但 package 版本保持不变。direct publish 要求 `image-id` 指向配置的版本来源镜像，因为创建 GitHub Release 必须有新的应用版本。

## Channel 选择规则

| Channel | 选择规则 |
|---|---|
| `stable` | 默认选择最高正式 SemVer，也可显式使用 Docker Hub `updated` 排序 |
| `beta` | 默认选择最高预发布 SemVer，也可显式使用 Docker Hub `updated` 排序 |
| `date` | 将日期格式标签映射为正式 SemVer，也可使用 `semver` 或 Docker Hub `updated` 排序 |
| `nightly` | 在正则匹配结果中选择目标平台 OCI 创建时间最新的镜像 |
| `custom` | 使用正则过滤，并显式选择 `semver`、`created` 或 `updated` 排序 |

Stable 示例：

```yaml
channel: stable
tag_regex: '^v?\d+\.\d+\.\d+$'
exclude_regex: 'windows|arm64'
```

筛选规则必须描述可持续增长的版本族，而不是当前看到的版本。例如只跟踪 v2 应使用 `tag_regex: '^v?2\.\d+\.\d+$'`；`tag_regex: '^2\.2\.0$'` 只会固定一个不可变标签，永远发现不了 2.2.1 或 2.3.0。精确筛选只适用于配合 digest 自增的 `latest` 等可变名称，或明确不需要自动更新的固定版本。候选筛选（`tag_regex`）、版本映射（`version_regex`/`version_template`）和排序（`sort`）必须分开设计。

Beta 示例：

```yaml
channel: beta
tag_regex: '^v?\d+\.\d+\.\d+-(alpha|beta|rc|preview)\.'
```

日期标签示例：

```yaml
channel: date
tag_regex: '^[0-9]{8}$'
version_regex: '^(?P<version>[0-9]{4})(?P<month>[0-9]{2})(?P<day>[0-9]{2})$'
version_template: '{version}.{month}.{day}'
```

`date` 与 `stable` 使用相同的正式版本排序，但会先应用配置的正则和模板映射。
模板展开后，SemVer 的三个数字核心字段会统一去掉前导零，因此
`20260626` 会映射为 `2026.6.26`，`20260101` 会映射为 `2026.1.1`。原有显式消费
补零捕获组的写法（例如 `0*(?P<build>[1-9]\d*)`）仍然兼容。

Docker Hub 更新时间优先示例：

```yaml
channel: stable
sort: updated
tag_regex: '^v?\d+\.\d+\.\d+$'
```

`updated` 使用 Docker Hub 标签元数据 `last_updated`，与 OCI `config.created` 不同：重新指向或重新推送已有标签时，`last_updated` 可以变化而镜像创建时间不变。时间相同先比较映射后的 SemVer，再比较标签名。该模式需要显式启用、目前仅支持 Docker Hub，并且绝不会回退使用创建时间。如果最近更新的标签映射到更低版本，现有 `allow_downgrade: false` 保护仍然生效。

Nightly 示例：

```yaml
channel: nightly
tag_regex: '^nightly(-.*)?$'
```

nightly 版本由选中目标平台镜像的创建时间和 digest 生成，结果仍是合法 SemVer：

```text
0.0.0-nightly.20260710153020.a1b2c3d4e5f6
```

### 可变标签与自动补丁版本

上游只发布 `latest` 等可变标签时，可显式启用基于 digest 的 patch 自增：

```yaml
update:
  strategy: publish
  allow_downgrade: false
  version_source:
    type: image
    image: app
    bump: patch

images:
  - id: app
    target: service
    service: app
    source: ghcr.io/acme/app
    channel: custom
    sort: created
    tag_regex: '^latest$'
    delivery:
      mode: lazycat
```

Action 会比较目标架构的上游 digest 与当前已交付镜像 digest。digest 相同即成功 no-op，包版本保持不变；digest 变化时只递增 patch（`1.4.6` → `1.4.7`），交付新镜像并进入正常的版本化 Release/商店流程。当前包版本必须是没有 prerelease/build metadata 的严格稳定 SemVer。`bump: patch` 不能与 `allow_downgrade`、标签版本映射或非 `custom`/非 `created` 规则组合。

mutable `direct` 和 `mirror` 引用会固定 digest，确保上次状态可追踪；mirror 必须设置 `require_digest_match: true`。官方商店仍强制使用 `delivery.mode: lazycat`。dry-run 会做同样的 digest 比较，但不会复制镜像或写文件。`image-results` 会输出 `currentDigest`、`sourceDigest`、`digestChanged`、`bump`、`previousVersion` 和 `selectedVersion` 供审计。

LazyCat 交付会把选中的源 digest 持久化到 Manifest 的 `upstream` 注释，并使用该 digest 固定的源引用执行远程复制。后续运行直接比较这个基线，不会匿名读取私有 LazyCat Registry。没有基线的旧 LazyCat 引用会做一次已认证、digest 固定的复制，并比较返回的内容寻址引用；外部运行镜像会先迁移到 LazyCat Registry，但不误加版本。只有“旧私有引用且尚无只读基线”的首次 dry-run 会失败关闭，完成一次可信非 dry-run 迁移后即可正常只读检查。

Custom 示例：

```yaml
channel: custom
sort: created
tag_regex: '^edge-'
version_regex: '^edge-(?P<version>\d+\.\d+\.\d+)$'
version_template: '{version}'
```

`version_template` 可以引用 `version_regex` 中的所有命名捕获组：

```yaml
version_regex: '^(?P<version>\d{8})\.0*(?P<build>[1-9]\d*)$'
version_template: '{version}.{build}.0' # 20260603.01 -> 20260603.1.0
```

`version` 捕获组仍然必填。未知占位符或展开后不是合法 SemVer 时会直接失败。配置了正则/模板映射时，三个 SemVer 数字核心字段会在校验前去掉前导零；预发布和构建标识仍遵循严格 SemVer 规则。

镜像仓库发现使用 `github.com/google/go-containerregistry`。Action 会先应用 `tag_regex` 和 `exclude_regex`。`max_tags` 限制 Registry 返回的原始标签列表，默认 `10000`；仅在已知的大型上游仓库中按镜像显式提高，最大 `50000`。`max_matching_tags` 独立限制筛选后的候选数，默认也是 `10000`，且不得超过 `max_tags`。SemVer 排序先按标签排名；`updated` 先按 Docker Hub 标签元数据排名，两者都按顺序检查 manifest，找到第一个可用的项目目标平台就停止。按创建时间排序因为目标镜像时间参与排名，仍必须检查全部候选 manifest。OCI index 和 Docker manifest list 只选择 `project.target_arch` 对应平台。默认降级保护可以防止旧版本映射静默降低应用版本。

## 镜像交付模式

### 复制到 LazyCat Registry

```yaml
delivery:
  mode: lazycat
```

Action 把选中的源镜像提交给懒猫开发者平台，并把 `Platform` 设置为 `project.target_arch`（默认 `amd64`，可选 `arm64`）。开发者平台执行远端 Registry-to-Registry 复制，返回最终的 `registry.lazycat.cloud/...` 地址。本地 Docker 不参与这次复制。

该模式优先使用 `LZC_API_TOKEN` 中的 PAT；历史 workflow 可以继续使用 `LAZYCAT_TOKEN` 中的 lzc-cli 会话 token。PAT 认证的 `LZC_API_HOST` 默认使用 `appstore.api.lazycat.cloud`，可通过环境变量覆盖。启用官方商店模式时只能使用这种交付方式。

### 可配置的 Registry 镜像源

```yaml
delivery:
  mode: mirror
  require_digest_match: true
```

省略 `image_template` 时，Docker Hub 默认使用 `docker.1ms.run`，GHCR 默认使用 `ghcr.1ms.run`。历史配置可以继续保留手写的 `image_template`；没有环境变量覆盖时不会改变。模板支持 `{tag}`、`{digest}` 和 `{source}`。启用 `require_digest_match` 后，Action 会检查 mirror 中与项目目标平台对应的镜像，只有 digest 与源镜像一致才会修改 Manifest。

mirror 校验不会复制镜像：其失败使用 `MIRROR_VERIFICATION_FAILED`，`IMAGE_COPY_FAILED` 仅用于懒猫 Registry 镜像交付。

Reusable workflow 会自动读取名为 `LAZYCAT_DOCKER_MIRROR`、`LAZYCAT_GHCR_MIRROR` 和 `LAZYCAT_REGISTRY_MIRRORS` 的 GitHub Repository/Organization Variables。未定义的 Variable 会解析为空字符串并安全 fallback。调用方也可以通过可选 workflow input 对单次调用进行覆盖：

```yaml
with:
  docker-mirror: mirror.example/docker
  ghcr-mirror: mirror.example/ghcr
  registry-mirrors: quay.io=mirror.example/quay,registry.example.com=mirror.example/registry
```

Composite Action 提供 `docker-mirror`、`ghcr-mirror` 和 `registry-mirrors` inputs，并 fallback 到同名 job/step 环境变量；composite metadata 不能访问 GitHub `vars` 上下文。需要 Repository/Organization Variables 的调用方必须在自己的 workflow 的 `with` 中映射，例如 `docker-mirror: ${{ vars.LAZYCAT_DOCKER_MIRROR }}`。值是不能带 URL scheme 的 Registry/path 前缀。优先级依次为 reusable/composite input（包括由调用方映射的 GitHub Variable）、composite 环境变量、历史 `image_template`、Docker Hub/GHCR 内置默认值。值缺失或为空不是错误。其他 Registry 必须在 `LAZYCAT_REGISTRY_MIRRORS` 中配置映射。

迁移历史项目时，mirror 模式可以从 Manifest 的 `upstream` 注释或已知的内置/自定义 mirror 地址恢复省略的 `source`。例如 `docker.1ms.run/acme/api:v1` 会恢复为 `docker.io/acme/api`。Action 不会回写 `.github/lazycat-action.yml`；无法无歧义恢复上游时，会在查询 Registry 或修改 Manifest 前安全失败。

### 直接使用源镜像

```yaml
delivery:
  mode: direct
```

Manifest 直接使用选中的源镜像，Action 不执行复制。这个模式适合私有商店，或者明确依赖外部 Registry 的部署。

官方商店模式会拒绝 `direct` 和 `mirror`。这两种方式只用于非官方分发。

## Runner 是否需要 Docker

| 场景 | Docker 要求 |
|---|---|
| 检查公开 OCI tag 和 manifest | 不需要 |
| LazyCat 远端镜像复制 | 不需要 |
| direct 或 mirror 地址更新 | 不需要 |
| reusable workflow 登录私有源 Registry | 需要 Docker CLI；GitHub 托管 Linux Runner 已安装 |
| 项目 buildscript 自己构建 Docker 镜像 | 需要 |
| ARM64 Runner 执行 x64 Dockerfile 的 `RUN` 步骤 | 需要 Docker Buildx 和 QEMU |

只有项目 buildscript 需要 Docker 时才选择 Docker 工具链：

```yaml
with:
  toolchains: docker
  enable-qemu: true
```

私有源 Registry 检查可以配置以下 GitHub Secrets：

```text
REGISTRY=ghcr.io
REGISTRY_USERNAME=<username>
REGISTRY_PASSWORD=<token or password>
```

reusable workflow 使用 `docker/login-action` 写入 Docker 凭据，OCI 客户端会读取这些凭据。它们只负责 Action 侧的镜像检查。lzc-cli 2.0.9 对应的 LazyCat `CopyImage` API 没有源 Registry 用户名、密码或 token 字段，因此 `mode: lazycat` 使用私有源镜像时，还要确保开发者平台本身能够拉取该镜像。

## 认证

LazyCat 镜像复制和官方 LPK 提交优先使用 PAT：

    export LZC_API_TOKEN='your-personal-access-token'

默认生产环境为：

    LZC_API_HOST=appstore.api.lazycat.cloud
    LZC_APPSTORE_COS_DOMAIN=dl.lazycat.cloud

历史 workflow 也可以继续使用原有 lzc-cli 会话 token：

    LAZYCAT_TOKEN='your-existing-lzc-cli-session-token'

两个变量选择的是不同认证协议，不是别名。`LZC_API_TOKEN` 使用 `/sdk/v3/developer` 和 `X-API-Token`；`LAZYCAT_TOKEN` 保留历史开发者接口和 `X-User-Token` 会话认证。两者同时存在时优先使用 `LZC_API_TOKEN`。`LZC_API_HOST` 只作用于 PAT API，默认使用 `appstore.api.lazycat.cloud`。`LZC_APPSTORE_COS_DOMAIN` 只用于 `skip_if_version_exists` 匿名查询，任何凭据都不会发送到该域名。域名覆盖值只填写域名，不包含协议和路径。

reusable workflow 会分别传递 `LZC_API_TOKEN` 和 `LAZYCAT_TOKEN`，由嵌套 Action 保留各自的协议语义；它不会创建、复制或同步 GitHub Secrets。历史 caller 可以只保留 `LAZYCAT_TOKEN`，新 caller 建议只配置 `LZC_API_TOKEN`。本地或直接调用 composite Action 时采用相同的优先级。任何凭据都不得写入仓库配置、workflow 普通输入或日志。

项目构建会执行仓库中的 buildscript。Action 会从 buildscript 环境中移除 LZC_API_HOST、LZC_APPSTORE_COS_DOMAIN、LZC_API_TOKEN、LAZYCAT_TOKEN、Registry 凭据、GitHub token，以及 GitHub output/control 文件路径。带写权限的发布 workflow 应只用于可信分支、tag、定时任务和手动运行，不要把 Secrets 暴露给不可信 Pull Request 代码。

## Pull Request 和 Release 工作流

### 默认安全流程：先创建 PR，合并后发布

定时检查使用前面的 `strategy: pull` 配置。再添加一个默认分支 caller：

```yaml
name: Publish merged LazyCat update

on:
  push:
    branches: [main]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
    secrets: inherit
```

更新 PR 合并后，默认分支 workflow 会重新构建 LPK。如果 `v<package version>` 还没有 Release，workflow 会创建 Release 并上传 LPK。同名 Release Asset 只有在 GitHub 返回的 SHA256 digest 一致时才会复用，digest 不同会直接失败。

### 直接发布

设置：

```yaml
update:
  strategy: publish
```

定时或手动镜像检查成功后，workflow 只提交受管的 package 和 Manifest 文件，commit 带 `[skip ci]`，然后推送当前分支、创建 `v<version>` 并上传 LPK。已有 tag 不会被移动；如果同名 tag 指向其他 commit，workflow 会失败。

直接发布会创建 Git commit、tag、GitHub Release 和 Release Asset。配置了商店时，reusable workflow 随后提交经过校验的 LPK。`strategy: pull` 永远不会发布商店。

定时或手动触发的自动直发开始前，如果启用了官方商店，Action 会通过带认证的开发者 API 查询精确的审核中版本。对于镜像版本源，`stores.official.continue_if_newer_version` 默认是 `true`：真实镜像检查会选择最新候选，并在 mirror 校验、懒猫镜像交付或文件修改之前完成比较。对于可变的 `bump: patch` 版本源，Action 根据 Manifest 中持久化的源 digest 规划候选应用版本：digest 未变时保留当前版本，变化时选择下一 patch。审核中版本等于或高于候选版本时暂停；候选版本更高时，后续自动升级和发布复用同一次选择继续执行。官方上传前，reusable workflow 还会用最终校验过的 LPK 版本再次执行认证比较，避免构建或创建 Release 期间新增的审核绕过门禁。将此选项设为 `false` 可恢复“只要存在审核就暂停”的保守行为。暂停结果会输出 `official-review-pending: true` 和 `official-review-version`；首次检查暂停时不会编辑、提交、推送、打 tag、创建 Release 或对账商店，最终复查暂停时则会阻止官方 LPK 上传和审核提交。Git 版本源没有可比较的上游镜像候选，因此存在审核时始终暂停。无效/非 SemVer 比较、认证错误和远程错误都会安全停止。显式 operation、dry-run、PR 更新及 Tag/Release 发布保持原行为。

## 商店发布

只有在 workflow 上传或安全复用 GitHub Release Asset，并确认 GitHub 返回的 SHA256 后，才会发布商店。没有 `services` 或 `images` 的静态 Web、Exec 应用同样使用这条发布链。

### 懒猫官方开发者平台

启用官方 lint 和发布：

```yaml
update:
  strategy: publish
  version_source:
    type: git

stores:
  official:
    enabled: true
    continue_if_newer_version: true
    skip_if_version_exists: true
    create_if_missing: true
    changelog_locales: [zh, en]
    retry:
      enabled: false
      max_attempts: 3
      initial_delay: 2s
      max_delay: 30s
    application:
      language: zh
      name: Example App
      brief: 面向协作 Agent 的专注工作空间
      description: 展示在应用详情页中的完整介绍。
      keywords: Agent, 协作, 工作空间
      source: https://github.com/acme/example
      source_author: acme
      support_pc: true
      support_mobile: true
      screenshot_pc_files:
        - .github/screenshots/pc-1.png
        - .github/screenshots/pc-2.png
      screenshot_mobile_files:
        - .github/screenshots/mobile-1.png
        - .github/screenshots/mobile-2.png
        - .github/screenshots/mobile-3.png
```

`create_if_missing: false` 只允许发布到已经存在的应用。允许创建时，`application.name` 默认读取 `package.yml.name`，`language` 默认为 `zh`。官方模式会执行与 lzc-cli 偏好一致的检查，包括 locales、图标不超过 200 KB、SemVer 元数据和 LazyCat Registry 运行镜像。`container_name` 等一般兼容性 warning 仍会展示，但不会阻断构建；只有被分类为官方商店 warning 的问题才会阻断官方发布，而且不会影响仅启用私有商店的 workflow。只要配置了 `direct` 或 `mirror`，就会在发布前失败。

应用信息字段都是可选附加参数。配置 `brief`、`description`、`keywords`、任一值为 true 的支持开关，或任一截图列表时，才会启用带认证的首次信息提交状态判断；只配置 `language`、`name`、`source`、`source_author` 时仍保持原有的仅创建应用行为。两个支持开关默认均为 false。Action 不通过匿名公开目录猜测首次提交，而是区分应用不存在、应用信息缺失、信息已审核和已有待审核任务。已审核的信息不会重复上传；存在待审核任务时，会在上传 LPK 或截图前失败。

截图文件必须先提交并推送到 workflow 会 checkout 的 ref。Agent 可以使用 `agent-browser` 按项目确认的桌面端和移动端 viewport 截图，保存到 `.github/screenshots/` 等仓库目录后再运行 Action。Action 不会下载远程截图 URL。PC 端需要 2-8 张，移动端需要 3-8 张；输入只能是 PNG/JPEG，单张不超过 15 MiB，宽高均须在 320-3840 像素之间。图片会自动居中裁剪为 16:9 并转成 PNG 上传。越出 `project.root` 的路径、符号链接、非普通文件和不安全文件名都会被拒绝；错误只会安全展示仓库相对路径和允许公开的原因，不暴露 runner 路径或凭据。

`skip_if_version_exists: true` 会在 LPK 校验完成后，通过精确包名匿名查询官方商店。版本相同时返回 `published: false`、`skipped: true` 和 `skipReason: version-already-online`。两者均为合法 SemVer、线上版本更高且 `update.allow_downgrade: false` 时，也会安全跳过并返回 `skipReason: online-version-newer`；只有显式设置 `allow_downgrade: true` 才继续执行回退提交。non-SemVer 值只判断精确相等，绝不按字符串猜测顺序。跳过时不会解析开发者 Token，也不会提交 LPK。匿名查询对无 HTTP 状态的连接错误、HTTP 429 和 HTTP 5xx 进行最多三次指数退避尝试；应用不存在时继续发布，其他错误或重试耗尽后安全失败。该选项默认 `false`，`dry-run` 仍然完全不发起远端请求。

设置 `LZC_APPSTORE_COS_DOMAIN` 时，版本查询使用该 COS 域名；未设置时使用生产目录。

官方发布始终把已验证的本地 LPK 文件作为 multipart 数据上传，绝不会把 GitHub Release URL 发送给官方平台。复用 Release Asset 时，会先把精确版本文件下载到项目目录下并重新校验。

官方重试为显式开启，默认 `enabled: false`。启用后，`max_attempts` 为 2-10 且包含首次尝试，`initial_delay` 与 `max_delay` 使用 Go duration 语法。审核前的安全重试会重新检查应用是否存在并重新打开 LPK，但凭据只解析一次。上传/检查阶段可重试无 HTTP 状态的连接/TLS/重置错误、HTTP 429 和 HTTP 5xx；审核创建只重试 HTTP 429。审核阶段的网络错误或 5xx 不会重放，因为服务端可能已经受理这个非幂等请求。取消、deadline 超时、鉴权、权限、NotFound、完整性错误、HTTP 400 和其他 4xx 都不重试。

失败会安全地区分 `store.official.upload` 与 `store.official.review`。Action 绝不打印原始响应正文；对合法 JSON 错误，只会显示经过单行化和长度限制的 `message`、`msg`、字符串 `error` 或嵌套的 `error.message`/`error.msg`，疑似凭据内容会被隐藏。双商店 reusable workflow 中，私有结果会被保留，官方失败降级为 warning，并写入 `store-results.official.failureReason: official-publish-failed`；如果官方商店是唯一目标，失败仍会使 workflow 失败。未启用官方商店时，不运行官方 lint 阻断、预检、凭据解析或发布。


### 喵喵私有商店

应用元数据可以写入配置，凭据不要提交到仓库：

```yaml
stores:
  official:
    enabled: false
  private:
    enabled: true
    skip_if_version_exists: true
    name: Example App
    summary: Published from CI
```

配置 GitHub Secrets：

```text
APPSTORE_URL=https://store.example.com
APPSTORE_TOKEN=lcst_...
APP_ID=42
PRIVATE_STORE_GROUP_CODES=ABC123,LATE23
```

`APP_ID` 和 `PRIVATE_STORE_GROUP_CODES` 都是可选项。分组码属于访问凭据，必须以逗号分隔的 GitHub Secret 保存。它只用于匿名查询线上最新版本，由 toolkit 默认通过 `X-Group-Codes` 请求头发送，不会进入 Action inputs、outputs、summary 或结果 JSON。toolkit 会清除 Cookie jar 并禁止重定向，防止分组码被转发到其他来源。

启用 `skip_if_version_exists: true` 后，Action 会在读取 `APPSTORE_TOKEN` 前通过精确包名查询喵喵商店。相等版本和线上更高的 SemVer 分别使用 `version-already-online`、`online-version-newer`，并且每个商店独立判断。无 HTTP 状态的连接错误、HTTP 429 和 HTTP 5xx 会进行最多三次指数退避尝试；应用不存在时继续发布，其他错误或重试耗尽后安全失败。真正发布时，如果没有 `APP_ID`，写客户端会先按 `packageId` 精确查找，再用 `stores.private.name` 调用带 Token 的 `GET /api/v1/apps/by-name?name=...` 接口。商店只返回当前 Token 有权上传版本的唯一精确同名应用；404 时创建新应用，同名歧义或鉴权错误直接停止。按名称解析出的历史应用可以保留不同的 `packageId`，Action 只使用其数字 ID 追加新的外部版本。提供 `APP_ID` 时，仍会先确认该应用的 `packageId` 与 LPK 一致。

### Release/商店对账

定时或手动触发的 `publish` workflow 也会对账 GitHub Release 与两个商店。如果镜像检查没有文件变化，但当前版本缺少 Release 或精确的版本化 Asset，reusable workflow 会执行一次恢复构建，验证 LPK，并补建 Release/Asset。如果当前 Tag 已有精确命名的 `<package-id>-v<version>.lpk`，但某个商店还没有该版本，则下载到项目根目录下，同时校验 GitHub 返回的 `sha256:` digest 与本地重新计算的 SHA256，再用同一份字节补交。已经存在该版本的商店会独立跳过，workflow 绝不会猜测其他文件或版本。

### GitHub Secret 作用域和优先级

reusable workflow 只按名称读取 GitHub Actions Secret，不区分它来自组织还是仓库。组织级 Secret 必须通过 repository access policy 授权给当前仓库，否则工作流无法读取。

同名 Secret 同时存在于多个层级时，更具体的层级优先：Environment Secret 覆盖 Repository Secret，Repository Secret 覆盖 Organization Secret。例如仓库级 `APPSTORE_URL` 会覆盖组织级同名值。组织 Secret 适合提供多个仓库共享的默认值；只有确实需要单仓库覆盖时才创建仓库级同名 Secret。不要在多个层级重复定义同名 Secret，除非这是有意的覆盖关系。

新建应用调用 `POST /api/v1/apps`；已有应用的外部版本调用 `POST /api/v1/apps/{APP_ID}/versions`，两者都发送 JSON。`downloadUrl` 和确认过的 64 位小写 `sha256` 都是必填项。reusable workflow 会把 GitHub 校验过的 SHA 传给发布操作，发布操作重新计算本地 LPK，任何不一致都会失败。URL 必须是真实的 `https://github.com/<owner>/<repo>/releases/download/...` Release Asset 地址。私有商店可以直接记录 Action 提供的 checksum，不需要仅为了重新计算 SHA256 而下载 LPK。相同版本和 SHA256 会幂等返回已有结果；同版本内容不同会失败。

私有商店支持 Docker 的 `lazycat`、`direct`、`mirror` 三种模式，也支持完全没有 Docker 镜像的应用。`direct` 和 `mirror` 应用不能误发官方商店。

## 静态、Exec、Go、Rust 和 TypeScript 的 tag/release 构建

没有 Docker service 的项目使用 Git 作为版本来源：

```yaml
update:
  strategy: pull
  version_source:
    type: git
```

tag 触发和 release 触发二选一。同一个 tag 同时启用两种触发方式会构建两次。

Tag 触发：

```yaml
name: Build tagged LPK

on:
  push:
    tags: ["v*"]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      toolchains: go
    secrets: inherit
```

Release 触发：

```yaml
name: Build released LPK

on:
  release:
    types: [published]

permissions:
  contents: write
  pull-requests: write

jobs:
  lazycat:
    uses: wcaqrl/lazycat-action/.github/workflows/lazycat.yml@v1
    with:
      operation: auto
      config: .github/lazycat-action.yml
      changelog: ${{ github.event.release.body }}
      toolchains: node
      node-package-manager: pnpm
    secrets: inherit
```

对于普通的 `v<version>` Tag，Action 会移除前导 `v`。显式提供版本时，匹配的 SemVer 事件 Tag 会原样保留。`client-v0.1.38`、`server-v0.1.44` 这类组件 Tag 必须以匹配的 `-v<version>` 结尾；Action 会保留事件 Tag 作为 Release 标识，并拒绝无关后缀或版本不一致。未显式提供版本时，事件 Tag 必须使用规范的 `v<version>`。随后 Action 更新 `package.yml.version`，运行项目 buildscript，构建并重新打开 LPK，执行 lint，计算 SHA256，然后上传到对应 Release。如果 tag/release checkout 修改了 `package.yml`，Release Asset 上传成功后，workflow 会把该文件同步回默认分支。

### TypeScript 静态 Web 构建

`lzc-build.yml`：

```yaml
buildscript: ./scripts/build.sh
contentdir: ./dist/content
```

`scripts/build.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail
npm ci
npm run build
rm -rf dist/content
mkdir -p dist/content
cp -R web-dist/. dist/content/
```

workflow 使用 `toolchains: node`，并传 `node-version` 或提交 `.node-version`。

如果 `.github/lazycat-action.yml` 同时声明了 `build.toolchains`，其中的工具链种类必须与 reusable workflow 输入一致。两边都显式填写版本时，版本也必须一致。
`build.toolchains[].version` 只支持 `go`、`node` 和 `rust`；`docker` 必须省略 `version`。

### Go Exec 构建

```bash
#!/usr/bin/env bash
set -euo pipefail
mkdir -p dist/content
CGO_ENABLED=0 \
GOOS="${LAZYCAT_TARGET_OS}" \
GOARCH="${LAZYCAT_TARGET_ARCH}" \
go build -trimpath -ldflags='-s -w' -o dist/content/app ./cmd/app
```

workflow 使用 `toolchains: go`，并传 `go-version` 或在 `go.mod` 中声明 Go 版本。

### Rust Exec 构建

```bash
#!/usr/bin/env bash
set -euo pipefail
cargo build --release --target x86_64-unknown-linux-gnu
mkdir -p dist/content
cp target/x86_64-unknown-linux-gnu/release/example dist/content/app
```

workflow 使用 `toolchains: rust`。可以传 `rust-toolchain`，也可以提交包含 `toolchain.channel` 的 `rust-toolchain.toml`。reusable workflow 会安装 `x86_64-unknown-linux-gnu` 和 `aarch64-unknown-linux-gnu`；buildscript 根据 `LAZYCAT_TARGET_ARCH` 选择 triple，并自行提供需要的交叉链接器。

### Docker buildscript

```bash
#!/usr/bin/env bash
set -euo pipefail
docker buildx build \
  --platform "${LAZYCAT_TARGET_PLATFORM}" \
  --load \
  -t example-build:local .
```

workflow 使用 `toolchains: docker`。ARM64 Runner 的 Dockerfile 构建阶段需要执行 x64 程序时，保留 `enable-qemu: true`。

可直接复制的完整文件位于 [`examples/`](examples/)：

- [`docker-stable-lazycat`](examples/docker-stable-lazycat/.github/lazycat-action.yml) 和 [`docker-mirror`](examples/docker-mirror/.github/lazycat-action.yml)
- [`go-exec`](examples/go-exec/.github/workflows/lazycat.yml) 和 [`rust-exec`](examples/rust-exec/.github/workflows/lazycat.yml)
- [`typescript-static`](examples/typescript-static/.github/workflows/lazycat.yml) 和 [`typescript-exec`](examples/typescript-exec/.github/workflows/lazycat.yml)
- [同时发布官方和私有商店](examples/stores/.github/workflows/lazycat.yml)

示例默认不映射 `LAZYCAT_TOKEN`。新 caller 使用 `LZC_API_TOKEN`；只有尚未迁移 PAT、仍明确依赖 lzc-cli 会话 token 的历史 workflow 才添加旧 Secret。不要把两个凭据同时映射为通用 fallback。

TypeScript Exec 示例要求锁文件中包含 `@yao-pkg/pkg`，并用 `node22-linux-x64` 演示默认的 `amd64` 目标。TypeScript 静态资源通常与 CPU 无关；Go、Rust、TypeScript Exec 和 Docker 构建必须遵循 `LAZYCAT_TARGET_ARCH`/`LAZYCAT_TARGET_PLATFORM`，选择 arm64 的项目需要匹配的工具链和打包运行时。

## 静态和 Exec Manifest 可以没有 services

静态 Web：

```yaml
application:
  subdomain: example
  routes:
    - /=file:///lzcapp/pkg/content
```

Exec：

```yaml
application:
  subdomain: example
  routes:
    - /=exec://8080,/lzcapp/pkg/content/app
```

这类项目不需要 `images` 配置，版本直接来自 tag 或 release。

## Outputs

| Output | 含义 |
|---|---|
| `operation` | 最终执行的 `check`、`build`、`publish-official` 或 `publish-private` 操作 |
| `changed` | 受管项目文件是否变化 |
| `package-id` | LazyCat package ID |
| `package-file` | `package.yml` 绝对路径 |
| `manifest-file` | Manifest 绝对路径 |
| `version` | 去掉前导 `v` 的规范化 SemVer |
| `tag` | 显式提供版本时保留匹配的事件 Tag；否则为规范化的 `v<version>` |
| `lpk-path` | 当前 job 中构建出的 LPK 绝对路径 |
| `sha256` | 64 位小写 LPK SHA256 |
| `download-url` | 发布后确认过的 GitHub Release Asset URL |
| `image-results` | 镜像选择和交付结果 JSON 数组 |
| `store-results` | 官方和私有商店发布结果 JSON 对象 |
| `official-store-enabled` | 配置是否启用官方商店 |
| `official-review-pending` | 是否因官方商店存在审核任务而暂停自动直发 |
| `official-review-version` | 当前官方商店审核中的精确版本号（如有） |
| `private-store-enabled` | 配置是否启用私有商店 |
| `update-strategy` | `pull` 或 `publish` |
| `channel` | 驱动应用版本的镜像 Channel |
| `result-file` | 完整且不含秘密的 JSON 结果文件 |
| `runner-arch` | `amd64` 或 `arm64` |
| `target-platform` | 默认 `linux/amd64`；设置 `project.target_arch: arm64` 时为 `linux/arm64` |

`image-results` 单项示例：

```json
{
  "id": "web",
  "target": "service",
  "service": "web",
  "platform": "linux/amd64",
  "tag": "v2.0.0",
  "sourceRef": "ghcr.io/acme/example-web:v2.0.0",
  "sourceDigest": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
  "deliveryMode": "lazycat",
  "deliveredRef": "registry.lazycat.cloud/acme/example-web:copy-id",
  "copied": true,
  "copyResult": {
    "sourceImage": "ghcr.io/acme/example-web:v2.0.0",
    "platform": "amd64",
    "lazyCatImage": "registry.lazycat.cloud/acme/example-web:copy-id",
    "finished": true
  }
}
```

完整结果写入 `.lazycat-action/result.json`。token、密码、Cookie 和 Authorization 请求头不会写入 outputs 或 step summary。

`store-results` 示例：

```json
{
  "official": {
    "published": true,
    "skipped": false,
    "created": false,
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "uploadUrl": "/developer/uploads/example.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  },
  "private": {
    "published": true,
    "skipped": false,
    "created": false,
    "existing": false,
    "appId": "42",
    "versionId": "56",
    "packageId": "cloud.lazycat.example",
    "version": "1.2.3",
    "onlineVersion": "1.2.2",
    "downloadUrl": "https://github.com/acme/example/releases/download/v1.2.3/app.lpk",
    "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}
```

发现相同线上版本时，对应商店结果改为 `published: false`、`skipped: true`，并且 `version` 与 `onlineVersion` 相同；不会读取写入凭据或调用提交接口。

## Artifact 和 Release Asset 的区别

- 只要本次运行生成了 LPK，就会上传 Workflow Artifact，便于 CI 检查。
- Pull Request 模式到 Artifact 和 PR 为止。
- Release 流程还会把 LPK 挂到 GitHub Release，并返回 `download-url`。
- 私有商店发布使用确认过的 Release Asset URL 和本地 SHA256，让商店直接信任提供的 digest，不必为了重新计算而下载文件。

## Dry run

```yaml
with:
  operation: check
  config: .github/lazycat-action.yml
  dry-run: true
```

Dry run 会选择版本并返回计划中的镜像地址，但不会复制镜像、修改文件、运行 buildscript、创建 PR 或创建 Release。

完整目标行为见[设计规格](docs/superpowers/specs/2026-07-10-lazycat-github-action-design.md)。
