# 让 AI 创建一个应用适配器

当前只针对 GitHub 项目和 GitHub Actions。给 AI 本仓库地址和下面的任务说明即可开始。它应先读
[AI 执行指南](../skills/lazycat-github-action/references/adapter-guide.md)，
再按需读[场景说明](../skills/lazycat-github-action/references/scenarios.zh-CN.md)
和[配置说明](../skills/lazycat-github-action/references/configuration.md)。

支持 Skill 的 AI 也可以使用整个 `skills/lazycat-github-action/` 目录；
只复制 `SKILL.md` 会丢失模板和参考资料。普通 AI 无需 Skill 安装功能，直接读取这些文件即可。

```text
请使用 https://github.com/wcaqrl/lazycat-action，先读取其中
skills/lazycat-github-action/SKILL.md 及 references/adapter-guide.md，
为下列项目创建或完善独立的懒猫应用适配仓库：

上游 GitHub 仓库：……
适配 GitHub 仓库与本地路径：……
懒猫应用包名、名称：……
商店已有 LPK：……（没有则写“新应用”）
目标架构：……
需要定制的功能：……（无则写“沿用官方镜像”）
凭据状态：……（只说明 Secret 名称，不把值写入代码或文档）
本次授权范围：仅分析 / 生成并测试 / 测试通过后提交并推送 / 真实送审。

核实稳定版本规则、镜像映射和实际部署要求，选择适配模式。
完整模板位于 skills/lazycat-github-action/assets/adapter/。
已有应用必须保留数据路径和安装参数；不能照搬示例版本或他人的状态锁。
先 dry-run，再做真实启动、持久化及必要的旧版升级测试。
只有在授权范围内才发布；最终提供验证证据、文件路径及操作结果。
```

不要要求 AI 在缺少实际构建方式、应用权限或凭据时虚构成功。AI 可以生成配置和脚本，但开发者 PAT、上架资料、协作者权限及项目特有的兼容性仍需真实确认。
