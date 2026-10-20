# 从空仓库创建一个应用适配器

本文只讨论 **GitHub 上游项目、GitHub 适配仓库和 GitHub Actions Runner**，适用于能在懒猫微服 Linux 环境运行的应用。上游源码不需要知道懒猫；你只维护自己的 `xxx-adapter`，调用公共的 `lazycat-action@v1`。无需 fork 执行器，也无需让本地电脑一直开机。

希望让 AI 代做时，使用 [AI 任务说明](ai-adapter-guide.md)。不同镜像与构建方式见[场景选择](../skills/lazycat-github-action/references/scenarios.zh-CN.md)。

运行配置仍以官方规范为准：[移植应用](https://developer.lazycat.cloud/app-example-porting.html)、[Manifest](https://developer.lazycat.cloud/spec/manifest.html)、[构建配置](https://developer.lazycat.cloud/build.html)。本工具负责自动化，不替代应用部署要求及商店审核标准。

## 1. 准备资料

| 必须确认 | 内容 |
|---|---|
| 上游 | GitHub 地址、正式版本 Tag 规则、部署文档 |
| 应用身份 | 你有权维护的包名、应用名称、自己的适配仓库 |
| 镜像或构建 | 官方镜像及目标架构，或可执行的源码构建方案 |
| 运行要求 | 端口、数据库、数据目录、启动参数、登录方式 |
| 已有商店应用 | 当前已发布 LPK、实际镜像版本、需要保留的数据与安装参数 |
| 发布资料 | 正确的图标、许可证、介绍；新建应用还需要真实截图等上架资料 |

不要把 LPK 文件名当作镜像实际版本的唯一证据。已有应用先解包，核对原镜像的版本与来源摘要，保留服务名、持久化路径和安装参数。新的注册、登录或免密适配应根据应用行为与[懒猫官方文档](https://developer.lazycat.cloud/)确定，不能套用固定管理员密码。

## 2. 复制完整模板

先在你自己的 GitHub 账号创建并 clone 一个空的 `xxx-adapter`。再取得本工具的源码，复制模板中的**所有文件，包括隐藏目录**：

```bash
git clone https://github.com/wcaqrl/lazycat-action.git /tmp/lazycat-action-guide
cp -R /tmp/lazycat-action-guide/skills/lazycat-github-action/assets/adapter/. /你的路径/xxx-adapter/
```

若临时路径已存在，使用已有 checkout 或换一个空路径，不要删除别人的目录。

复制后应有：

```text
xxx-adapter/
├── .github/workflows/update.yml  # 定时、手动触发及 @v1 调用
├── .gitignore
├── README.md
├── icon.png                     # 512×512 占位图，必须替换成实际应用图标
├── package.yml                  # 包名、版本、介绍、许可证等
├── lzc-build.yml                # 指定 Manifest、图标和输出位置
├── lzc-manifest.yml             # 服务、路由、持久化等
└── lazycat-action.yml           # 上游、版本规则、镜像和送审配置
```

按应用增加 `image/`、`scripts/`、`tests/`、截图和 `lzc-deploy-params.yml`。安装参数文件必须在 `lzc-build.yml` 的 `deploy_params` 中声明。无需 `lzc-build.dev.yml`。

## 3. 替换应用配置

模板默认适用于“Git 稳定 Tag + 对应官方版本镜像”。它不是可以直接安装的示例应用。

| 文件 | 需要修改 |
|---|---|
| `package.yml` | 包名、名称、介绍、主页、作者、实际许可证、支持的平台 |
| `lzc-manifest.yml` | 子域名、路由中的包名、服务端口、容器数据目录、启动要求 |
| `lazycat-action.yml` | 输出包名、上游地址、Tag 规则、镜像模板、目标架构 |
| `icon.png` | 替换占位图；不要把占位图提交上架 |
| `README.md` | 首次使用、持久化、验证方式与应用特有的限制 |

新应用以 `version: 0.0.0` 作为未发布起点；已有商店应用填写真实的已发布版本，不能退回 `0.0.0`。源版本与懒猫应用版本可能不同，例如修复被拒绝的版本或上游使用四段版本号时。

特别核对镜像 Tag：`{tag}` 保留上游 Git 标签（如 `v1.2.3`），`{source_version}` 是归一化版本（如 `1.2.3`）。不能默认 Git 和镜像标签完全同名。单个应用选择一个权威来源；有稳定 Tag 时通常不需要监控分支。

`github-release` 更新日志要求该 Git Tag 存在已发布的正式 GitHub Release；只有 Tag 没有 Release 正文时，改用 `git-commits`。GitHub Release 正文模式不会自动把镜像 Tag `1.2.3` 改成 Git Tag `v1.2.3`。

## 4. 配置商店和仓库权限

先确认你是目标包名对应应用的所有者或协作者。

- 已有应用：保持 `create_if_missing: false`，不要新建另一个同名包来绕过权限。
- 新应用：可以先在开发者平台创建应用资料，然后保持该值为 false；也可以按[自动建档说明](../skills/lazycat-github-action/references/configuration.md#new-official-application)配置 `create_if_missing: true`、介绍及真实截图。

在 **适配仓库 → Settings → Secrets and variables → Actions → Repository secrets** 添加：

| Secret | 用途 |
|---|---|
| `LZC_API_TOKEN` | 你的懒猫开发者 PAT，用于官方接口鉴权，不是 Docker 仓库密码 |
| `SOURCE_SSH_KEY` + `SOURCE_KNOWN_HOSTS` | 仅私有 Git 源需要，或改用 `SOURCE_TOKEN` + `SOURCE_USERNAME` |
| `REGISTRY`、`REGISTRY_USERNAME`、`REGISTRY_PASSWORD` | 仅需要向中转仓库推送自定义镜像时配置 |

不需要把这些 Secret 放进上游项目或公共 `lazycat-action` 仓库，也不要写进任何 YAML 值中。转存后的官方镜像地址使用 API 最终返回值，不手工拼接他人的 `peter/` 命名空间。

模板已经声明 `contents: write` 和 `pull-requests: write`。如果组织策略或分支保护阻止写入，按仓库政策允许相应更新方式，或使用工具支持的 PR 策略；不要直接关闭保护规则。工作流必须先推送到默认分支，才能按预期定时运行。

## 5. 先检查和测试

提交并推送适配代码后，配置变更触发的任务只做版本计划检查。手动入口：

**Actions → Update application for LazyCat → Run workflow**，保持 `dry-run` 勾选。

核对选中的上游版本、Git revision、目标架构、镜像映射及本次更新日志。只读检查不会运行 `build.prepare`，因此它不能证明源码构建、镜像拉取或数据迁移成功。

本地已有 CLI 时可运行：

```bash
lazycat-action run --operation check --config lazycat-action.yml --dry-run
```

**`check` 不等于只读；必须有 `--dry-run`。** 配置了 `changelog.mode: command` 时，该命令在 dry-run 中也会执行，因此应只读。首次接入通常直接使用 GitHub Runner，不要求本机安装 `lzc-cli`；执行器通过内置的 LPK 构建库打包。

加入真正的应用测试：新应用至少验证启动、页面/API 和持久化；已有应用验证从商店旧版升级后的用户数据、配置、登录和重启。测试应绑定选中的版本和摘要，不能另取 `latest`。不同应用需自行实现测试脚本，模板没有伪造一个“通用升级测试成功”。

## 6. 第一次真实送审

应用测试通过、你已经决定提交审核后，在同一入口取消 `dry-run`，保持 `retry-rejected` 不勾选。

正式流程会根据配置转存/构建镜像，生成并校验 LPK，将版本、官方镜像与状态锁提交回适配仓库；随后并行运行官方送审和 GitHub Release 发布。

验收时分别检查：

1. 测试通过，转存任务完成并取得真实官方镜像地址。
2. LPK 内含正确的图标、包名、版本、Manifest 和必要的安装参数。
3. 送审 Job 成功，状态锁有真实的审核 ID、提交时间及状态；打包成功不代表审核已提交，更不代表审核已通过。
4. `v<应用版本>` Release 能下载 `<包名>-v<应用版本>.lpk`，附件摘要与送审文件一致。只上传 LPK；GitHub 自动生成的源码 ZIP/TAR 不由本工具上传。

## 7. 开启长期自动更新

新模板每天北京时间 **11:17** 检查，GitHub 可能延迟。定时任务起初也只检查；首次验证完毕后，在 Actions 的 **Variables**（不是 Secrets）添加：

```text
Name:  LAZYCAT_AUTO_PUBLISH
Value: true
```

此变量只控制新模板的定时任务。人工取消 `dry-run` 不依赖该变量；已有适配器的工作流不会被本次模板修改自动改变。执行器不是常驻服务器，定时规则在每个适配仓库中。

`.lazycat-action.lock.yml` 由流程生成并提交，用于恢复中断任务和防止重复送审。不要忽略、删除、复制他人锁文件或手工填 `submitted`。被拒绝的同一个源版本不会每天再次送审；修复后需要主动取消 `dry-run` 并勾选 `retry-rejected`，分配新的应用 patch。新的上游版本仍可替代旧的待审核版本。

LPK Release 发布失败不影响送审，可单独修复并重试 Release Job。保留 `@v1` 即可接收工具的 v1 兼容更新，无需逐个改为 `v1.8.0` 等小版本。
