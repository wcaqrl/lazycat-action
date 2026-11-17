# GitHub 元数据读取（v1.10.0+）

适用于需要在自定义更新日志、镜像准备或打包脚本中读取 GitHub Release/标签的
GitHub 适配器。普通适配器的配置和 Secrets 不变，仍引用公共 `@v1`。

## 凭据与调用方式

通用工作流把 Runner 自动生成的 `GITHUB_TOKEN` 交给执行器。执行器在本机
`127.0.0.1` 启动临时 HTTP 读取通道，只允许配置中 `source.url` 或
`changelog.git_url` 对应的 GitHub 仓库，仅开放 GET Release/标签元数据。
脚本获得 `LAZYCAT_GITHUB_API_URL` 和短时 `LAZYCAT_GITHUB_API_SESSION`，
真实 GitHub Token、商店 PAT 和仓库密码仍不传入这些脚本或其构建容器。
通道在本次执行结束或失败时关闭，不把会话写入状态或结果。

将 [Python 客户端](../clients/python/github_api.py) 原样复制到适配器的
`scripts/github_api.py`，在同目录的脚本中使用：

```python
from github_api import github_json

release = github_json(
    'https://api.github.com/repos/example/application/releases/latest'
)
```

支持 `/releases`（数字 `page`/`per_page`，1–100）、`/releases/latest`、
`/releases/tags/TAG`、`/git/ref/tags/TAG` 和 `/git/tags/SHA`。
这不是任意 URL 代理；不支持写请求、跨仓库读取、重定向或容器直接连接。
含 `/` 的标签尚不在该通道范围内。非 GitHub/纯 OCI 配置不会启动通道。

可信的独立工作流验证步骤不经过执行器，可以显式设置
`GITHUB_TOKEN: ${{ github.token }}` 后使用同一客户端。CI 中缺少 Token 和
读取通道时明确报错；本机交互式检查仍可匿名读取公开元数据。
自动 Token 不赋予其它私有仓库的访问权限；已有私有 Git SSH/Token 配置不变。

## 错误和版本一致性

共享 Go 读取器和独立 Python 客户端最多请求三次，对 429、明确的配额 403、
暂时网络错误及 500/502/503/504 进行重试，累计等待最多 30 秒。
遵守配额重置/重试时间，超出预算则失败，留给下次定时任务重试。
401、权限 403、404 和重定向直接失败；不输出响应正文或凭据。
通道内由执行器重试，Python 不叠加重试。不会缓存可变化的标签和 Release。

此功能不会替代正式 Release、预发布、提交号、平台或镜像摘要校验。
获取 Git 标签提交号优先使用 `git ls-remote`，同时查询标签及其 `^{}`
以正确处理附注标签；避免为了基线提交多请求一次 Release API。
内置 `changelog.mode: github-release` 也使用共享重试读取器，原有输出保持不变。
