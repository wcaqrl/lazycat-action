# 选择适配方式

本文使用 GitHub 上游仓库、GitHub 适配仓库和 GitHub Actions。先复制[完整模板](../assets/adapter/)，再替换必要文件。以下均为需要按实际应用填写的通用模板，不是可直接安装的完整产品。适配器负责应用知识，执行器不猜测上游的 Dockerfile、数据迁移或登录方式。

| 目标情况 | 采用方式 | 模板 |
|---|---|---|
| Git Tag 发布，已有对应版本镜像 | Git `semver-tag` + `images` | 完整模板默认配置 |
| 以官方镜像版本为准 | OCI 稳定版本标签 + `passthrough` | [oci.yml](../assets/recipes/oci.yml) |
| 一个 Tag 对应多个容器 | Git Tag + `images` | [multi-image/](../assets/recipes/multi-image/) 整个目录 |
| 基于上游镜像加入工具、插件或配置 | OCI + `dockerfile` | [custom-image.yml](../assets/recipes/custom-image.yml) 及 [image 文件](../assets/recipes/custom-image/) |
| 没有镜像，从源码构建 | Git + `command`/`dockerfile` | [source-build.yml](../assets/recipes/source-build.yml) |
| 必须验证候选镜像后才打包 | OCI + `command`，或 Git + 自定义校验 | 按下文加入应用测试 |

## Git Tag 与镜像的关系

版本发现和镜像获取是两件事。Git 源能先发现新 Tag，再拉取对应镜像或自己构建。上游没有发布镜像时，需有真实可用的构建步骤；镜像暂未就绪则本次失败或等待后续重试，不能替换成别的版本。

`{tag}` 保留标签前缀，`{source_version}` 去除归一化的 `v` 前缀。Git `v1.2.3` 可以映射镜像 `1.2.3`。`github-release` 日志必须存在同名正式 Release；OCI 标签和 Git Release 标签不一致时，使用能匹配 `v` 前缀的 `git-commits`，或由只读日志命令验证实际对应关系。

标准 SemVer 用 `semver-tag`。像 Plex 那样四段数字加哈希的标签，需要显式 `sort: numeric`、`version_regex`、`version_template`，完整源版本仍是比较依据；不能丢掉第四段后比较，也不能用应用版本代替源版本。详细字段见[配置说明](configuration.md#numeric-vendor-versions)。

## 多镜像

复制 `multi-image/` 下所有文件覆盖基础模板，保留基础模板的图标、Package、工作流和忽略规则。它增加服务、安装参数及显式收集配置。按上游要求修改各服务的连接参数、数据目录和依赖镜像版本。

应用和 worker 使用同一个源版本占位符；数据库可以独立固定兼容版本/摘要，不能把应用的版本标签套给 PostgreSQL。所有绑定先完成目标平台解析和转存，再整体修改 Manifest。每个 `images[].service` 必须对应实际服务。

## 自定义镜像

用 `custom-image.yml` 覆盖 `lazycat-action.yml`，将 `custom-image/` 复制成适配仓库中的 `image/`。工作流调用增加：

```yaml
with:
  toolchains: docker
```

`ARG SOURCE_IMAGE` / `FROM ${SOURCE_IMAGE}` 使用带摘要的上游基础镜像。示例只复制一个文件，不能代表完成了 Java、Python、插件等业务配置。实际安装命令要匹配基础镜像发行版、包管理器、用户及启动机制。

`output_image` 必须是 Runner 能推送且执行器能读取的远程镜像，不是仅存在于 Runner Docker daemon 的本地 tag。配置 Registry Secrets；中转源还需让官方 copy-image 服务读取。开发者 PAT 不能代替 Docker 登录。

## 源码构建

`source-build.yml` 适用于上游根目录已有可用 Dockerfile 的情况；它精确 checkout 候选 Git revision，再通过 `LAZYCAT_SOURCE_DIR` 构建并推送。没有 Dockerfile 时必须按项目技术栈补齐真实的构建脚本或适配仓库 Dockerfile，不使用缺失的示例脚本冒充成功。

示例选 `branch-head/auto` 展示没有 Tag 的情况；有正式 Tag 时，应保留 Git 源而改为 `semver-tag`。私有源按[凭据说明](configuration.md#private-git-sources)设置 `auth_ref`。分支更新只表示新 commit，不保证必然有对应镜像。

## 应用测试与镜像一致性

测试应在打包/送审之前运行。可把应用测试作为适配工作流的前置 Job，再通过 `needs` 约束调用；注意分别发现“最新版本”可能导致测试候选与打包候选不同。

更严格的 OCI 流程可把测试放入 `build.prepare`：

```yaml
build:
  run_buildscript: false
  prepare:
    mode: command
    context: scripts
    command: ./scripts/test-upgrade.sh
    output_image: '{source_ref}@{source_revision}'
```

该命令需要由适配器实际实现，使用 `LAZYCAT_SOURCE_REF` 和 `LAZYCAT_SOURCE_REVISION` 测试不可变镜像。Git 源的 revision 是 Git SHA，**不能**拿它拼接 OCI 摘要；应解析对应镜像，记录测试摘要，并在构建脚本中与转存后的源摘要核对。

已有应用测试应覆盖旧版创建的代表性数据、数据库迁移、配置、登录和容器重建。仅 healthcheck 成功或空数据库启动不能证明升级兼容。测试数据和凭据应是临时的，不依赖真实用户数据。

## 私有源与中转

私有 GitHub 源仓库需只读 SSH Key + 可信 known_hosts，或 HTTPS Token。Private Git changelog 建议使用 `git-commits` 并设置同一 `auth_ref`；不要假设 SSH Key 会自动变成 GitHub Release API 的登录凭据。

官方服务不能拉取私有 Registry 时，选择能匿名读取的受控发布源、摘要一致的 `copy_source`，或配置 `staging_image` 并在工作流启用 `enable-image-staging: true`。源码访问凭据、镜像仓库凭据、懒猫 PAT 是三种不同权限。

## 新应用与已有应用

已有应用保留真实基线并使用 `create_if_missing: false`。新应用可先人工建档，或按配置说明自动建档并提交真实介绍及截图。不要照搬其它适配器的包名、管理员密码、镜像个人命名空间、状态锁或无关的移动端限制。
