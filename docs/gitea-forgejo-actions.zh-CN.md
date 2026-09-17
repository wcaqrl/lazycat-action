# 在 Gitea Actions 和 Forgejo Actions 中使用 LazyCat GitHub Action

[English](gitea-forgejo-actions.md)

本仓库提供的 composite Action 可以在 Gitea Actions 和 Forgejo Actions 中运行。两个平台都支持 composite Action、完整 Action URL、`github` 上下文，以及本项目使用的 `GITHUB_*` 兼容环境变量。

请直接调用 composite Action。`.github/workflows/lazycat.yml` reusable workflow 包含 GitHub 专属的 Pull Request、Artifact、Release 和 API 操作，不能原样迁移。

## 支持范围

| 操作 | Gitea Actions | Forgejo Actions | 说明 |
|---|---|---|---|
| `check` | 支持 | 支持 | 在任务工作区内更新受管文件。创建 Pull Request 需要平台专用的 workflow 步骤。 |
| `build` | 支持 | 支持 | 生成经过校验的 LPK 和 SHA256。Artifact 上传由调用方 workflow 负责。 |
| `publish-official` | 支持 | 支持 | 把已有且经过校验的 LPK 提交到懒猫官方开发者平台。 |
| `publish-private` | 有限制 | 有限制 | 当前客户端只接受真实的 `https://github.com/.../releases/download/...` Asset URL，Gitea 或 Forgejo 原生 Release URL 会被拒绝。 |
| `auto` | 支持，但建议显式配置 | 支持，但建议显式配置 | Runner 会提供 GitHub 兼容的事件和 ref 变量，但显式指定操作更便于跨平台检查。 |

## Runner 要求

任务运行环境必须满足以下条件：

- 使用 Linux amd64 或 arm64。
- Runner 镜像或主机中包含 Bash、curl、tar、grep 和 sha256sum。
- Runner 支持当前官方 GitHub Actions 所需的 Node.js 24 Action 运行时。
- 调用 Action 前已经 checkout 项目工作区。
- 使用默认启动方式时，Runner 可以访问源镜像仓库、懒猫服务和 GitHub Releases。

Composite Action 不会安装项目构建工具链。如果配置中的 `buildscript` 需要 Go、Node.js、Rust、Docker 或其他编译器，请在调用 Action 前安装，或者把它们加入 Runner 镜像。

如果希望继续使用默认配置路径，可以把项目配置保留在 `.github/lazycat-action.yml`。Workflow 文件需要放在 Gitea 的 `.gitea/workflows/` 或 Forgejo 的 `.forgejo/workflows/` 中。

## Gitea Actions

在应用仓库中创建 `.gitea/workflows/lazycat.yml`：

```yaml
name: Build LazyCat LPK

on:
  push:
    tags:
      - "v*"

jobs:
  build:
    # 请替换为当前 Gitea 实例中已配置的 Linux Runner 标签。
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: https://github.com/actions/checkout@v7
        with:
          fetch-depth: 0

      - name: Build LPK
        id: lazycat
        uses: https://github.com/wcaqrl/lazycat-action@v1
        with:
          operation: build
          config: .github/lazycat-action.yml
          version: ${{ github.ref_name }}

      - name: Show build result
        env:
          LPK_PATH: ${{ steps.lazycat.outputs.lpk-path }}
          LPK_SHA256: ${{ steps.lazycat.outputs.sha256 }}
        run: |
          test -f "${LPK_PATH}"
          echo "LPK: ${LPK_PATH}"
          echo "SHA256: ${LPK_SHA256}"
```

Gitea 会根据实例配置的默认 Action 地址解析没有主机名的 `uses`。上面的完整 URL 明确指定从 GitHub 下载，即使实例把默认地址设为 Gitea 本身也不会产生歧义。

## Forgejo Actions

在应用仓库中创建 `.forgejo/workflows/lazycat.yml`：

```yaml
name: Build LazyCat LPK

on:
  push:
    tags:
      - "v*"

jobs:
  build:
    # 请替换为当前 Forgejo 实例中已配置的 Linux Runner 标签。
    runs-on: docker
    steps:
      - name: Checkout
        uses: https://github.com/actions/checkout@v7
        with:
          fetch-depth: 0

      - name: Build LPK
        id: lazycat
        uses: https://github.com/wcaqrl/lazycat-action@v1
        with:
          operation: build
          config: .github/lazycat-action.yml
          version: ${{ github.ref_name }}

      - name: Show build result
        env:
          LPK_PATH: ${{ steps.lazycat.outputs.lpk-path }}
          LPK_SHA256: ${{ steps.lazycat.outputs.sha256 }}
        run: |
          test -f "${LPK_PATH}"
          echo "LPK: ${LPK_PATH}"
          echo "SHA256: ${LPK_SHA256}"
```

Forgejo 通常会根据实例配置的默认 Action 地址解析没有主机名的 Action。需要使用 GitHub 上的 Action 时，建议写完整 URL。

## 定时检查镜像版本

下面的任务可以用于两个平台。请把文件放入对应平台的 workflow 目录，并选择有效的 Linux Runner 标签：

```yaml
name: Check LazyCat images

on:
  schedule:
    - cron: "17 3 * * *"
  workflow_dispatch:

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: https://github.com/actions/checkout@v7
        with:
          fetch-depth: 0

      - name: Check image versions
        id: lazycat
        uses: https://github.com/wcaqrl/lazycat-action@v1
        with:
          operation: check
          config: .github/lazycat-action.yml
          dry-run: "false"

      - name: Report changes
        env:
          CHANGED: ${{ steps.lazycat.outputs.changed }}
          VERSION: ${{ steps.lazycat.outputs.version }}
        run: echo "changed=${CHANGED} version=${VERSION}"
```

`check` 可以修改工作区中的 `package.yml` 和配置的 Manifest。直接调用 Action 时，它不会提交、推送或创建 Pull Request。如果需要保留这些修改，请增加 Git 命令或平台原生的 Pull Request Action。

只想计算更新结果，不希望修改工作区文件或远程状态时，请设置 `dry-run: "true"`。

## 发布到懒猫官方开发者平台

Action 配置必须启用直接发布和官方商店：

```yaml
update:
  strategy: publish

stores:
  official:
    enabled: true
```

先构建 LPK，再把经过校验的路径和 SHA256 传给第二个 Action 步骤：

```yaml
- name: Build LPK
  id: lazycat-build
  uses: https://github.com/wcaqrl/lazycat-action@v1
  with:
    operation: build
    config: .github/lazycat-action.yml
    version: ${{ github.ref_name }}

- name: Publish to the LazyCat official platform
  id: lazycat-publish
  uses: https://github.com/wcaqrl/lazycat-action@v1
  env:
    LZC_API_HOST: ${{ secrets.LZC_API_HOST }}
    LZC_API_TOKEN: ${{ secrets.LZC_API_TOKEN }}
  with:
    operation: publish-official
    config: .github/lazycat-action.yml
    version: ${{ steps.lazycat-build.outputs.version }}
    changelog: Release ${{ steps.lazycat-build.outputs.version }}
    lpk-path: ${{ steps.lazycat-build.outputs.lpk-path }}
    sha256: ${{ steps.lazycat-build.outputs.sha256 }}
```

懒猫开发者平台优先使用保存在 Secret 管理功能中的 PAT `LZC_API_TOKEN`。历史自动化也可以继续把 lzc-cli 会话 token 作为 `LAZYCAT_TOKEN` 传入；两个名称选择不同的认证协议，同时设置时优先使用 `LZC_API_TOKEN`。`LZC_API_HOST` 仅用于覆盖 PAT API 的生产默认域名。

## 启动文件下载和内网环境

默认情况下，`scripts/run-action.sh` 会从本 Action 的 GitHub Release 下载以下文件：

```text
lazycat-action_linux_amd64.tar.gz
lazycat-action_linux_arm64.tar.gz
checksums.txt
```

取得 Action 仓库和下载 Release 二进制是两个独立的网络操作。完全无法访问 GitHub 的实例需要先把 Action 仓库镜像到 Gitea 或 Forgejo，并改用镜像仓库 URL：

```yaml
- uses: https://forge.example.com/ci/lazycat-github-action@v1
```

`actions/checkout` 也需要镜像，或者改用本地 checkout 步骤。

如果任务环境无法访问 GitHub Releases，还需要镜像发布文件，并在 Action 步骤中设置下载地址：

```yaml
- uses: https://forge.example.com/ci/lazycat-github-action@v1
  env:
    LAZYCAT_ACTION_RELEASE_BASE_URL: https://downloads.example.com/lazycat-action/v1
  with:
    operation: build
    config: .github/lazycat-action.yml
    version: ${{ github.ref_name }}
```

下载目录必须包含与 Runner 架构对应的压缩包和原始 `checksums.txt`。浮动 `v1` 标签指向新版本后，需要替换这些文件。

Self-hosted Runner 也可以提供预安装的二进制：

```yaml
env:
  LAZYCAT_ACTION_BINARY: /opt/lazycat/bin/lazycat-action
```

这个路径必须指向可执行的普通文件，不能是符号链接。

## 已知限制

### 不要原样调用 GitHub reusable workflow

`.github/workflows/lazycat.yml` 使用 GitHub 专属的 Action 和 API 处理 Artifact、Pull Request、Release、Release Asset digest 和仓库内容更新。Gitea 和 Forgejo 可以解析很多 GitHub 风格的 workflow 语法，但远程 API 和具体 Action 实现并不通用。

请用目标平台的原生步骤重新编排这些操作，并继续通过 composite Action 执行 LazyCat 业务逻辑。

### 原生 Release Asset 不能用于私有商店发布

`publish-private` 当前要求 `download-url` 必须指向 `https://github.com/<owner>/<repo>/releases/download/...`。即使文件内容和 SHA256 正确，Gitea 或 Forgejo Release Asset URL 也无法通过校验。

只有传入真实的 GitHub Release Asset 时，才能从 Gitea 或 Forgejo Runner 执行这个操作。支持原生 Release URL 需要修改本项目代码。

### Artifact 和 Pull Request 由调用方负责

直接调用 Action 会返回 `lpk-path`、`sha256`、修改过的文件路径，以及主 README 中列出的其他 outputs。后续操作请使用目标平台的 Artifact、Release、仓库和 Pull Request API。

## 常见问题

- 出现 `lazycat-action supports Linux runners only`：请选择 Linux Runner。
- 出现 `unsupported runner architecture`：请使用 amd64 或 arm64。
- 二进制启动前下载失败：允许访问 GitHub Releases，或者配置 `LAZYCAT_ACTION_RELEASE_BASE_URL` 或 `LAZYCAT_ACTION_BINARY`。
- 找不到配置文件：先 checkout 仓库，并检查 `config` 路径。
- 项目 buildscript 找不到 Go、Node.js、Rust 或 Docker：在 Action 步骤前安装所需工具链。
- `auto` 选择了意外的操作：显式设置 `operation` 和 `version`。
- 旧版 Runner 中步骤 outputs 为空：升级 Gitea act runner 或 Forgejo Runner，确保它提供 `GITHUB_OUTPUT` 兼容变量。
- 私有商店发布拒绝下载地址：改用 GitHub Release Asset URL，或者禁用该发布目标。

## 平台文档

- [Gitea Actions 概览](https://docs.gitea.com/usage/actions/overview)
- [Gitea 与 GitHub Actions 的差异](https://docs.gitea.com/usage/actions/comparison)
- [Gitea Actions 变量](https://docs.gitea.com/usage/actions/actions-variables)
- [Forgejo Actions 参考文档](https://forgejo.org/docs/latest/user/actions/)
