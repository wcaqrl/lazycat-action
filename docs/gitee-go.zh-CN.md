# 在 Gitee Go 中运行 lazycat-action

`lazycat-action` 的核心是 Linux CLI，不依赖 GitHub 事件。Gitee Go、自建 Runner、Jenkins 或系统 cron 都可以显式执行 `run` 命令。

```bash
export LAZYCAT_CONFIG=lazycat-action.yml
export LAZYCAT_OPERATION=auto
export LZC_API_TOKEN='${GITEE_LZC_API_TOKEN}'
./scripts/gitee-run.sh
```

`gitee-run.sh` 默认在 `check` 生成新 LPK 后立即执行 `publish-official`，并在提交前再次检查是否已有版本处于审核中。只想生成和校验 LPK 时，设置 `LAZYCAT_PUBLISH_AFTER_CHECK=false`。

Gitee 私有源仓库推荐使用只读部署公钥。配方使用统一的逻辑凭据名：

```yaml
source:
  kind: git
  url: git@gitee.com:example/private-app.git
  auth_ref: source
  select:
    strategy: branch-head
    branch: auto
```

Gitee Go 的加密变量映射为：

```bash
export LAZYCAT_AUTH_SOURCE_SSH_KEY="${GITEE_SOURCE_SSH_KEY}"
export LAZYCAT_AUTH_SOURCE_KNOWN_HOSTS="${GITEE_SOURCE_KNOWN_HOSTS}"
```

HTTPS Token 方式使用：

```bash
export LAZYCAT_AUTH_SOURCE_TOKEN="${GITEE_SOURCE_TOKEN}"
export LAZYCAT_AUTH_SOURCE_USERNAME="${GITEE_SOURCE_USERNAME}"
```

凭据只在 `git ls-remote` 和精确 revision checkout 阶段使用。SSH 私钥写入权限为 `0600` 的临时目录，任务结束后删除；Token 通过 `GIT_ASKPASS` 提供，不写入仓库 URL。

也可以在 Gitee Go 中把构建和发布拆成两个任务：

1. 定时任务执行 `LAZYCAT_OPERATION=check`，发现指纹变化后构建镜像和 LPK。
2. 使用同一个任务继续执行 `publish-official`，或者直接让调度脚本在 `check` 成功后调用：

```bash
lazycat-action run \
  --operation publish-official \
  --config lazycat-action.yml \
  --version "${VERSION}" \
  --lpk-path "${LPK_PATH}" \
  --sha256 "${LPK_SHA256}" \
  --changelog "Release ${VERSION}"
```

发布目标只有懒猫官方应用商店。Gitee 仅负责代码托管与运行流水线。
