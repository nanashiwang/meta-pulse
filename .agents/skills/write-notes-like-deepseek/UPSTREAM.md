# 来源与项目适配

来源：https://github.com/czm15053/write-notes-like-deepseek
固定提交：`8f3b404cf9fd8102f96561f390219877abf094a8`（2026-09-29 引入）。
上游 README 声明 MIT；该提交未提供独立 LICENSE 文件，此处不伪造版权或许可文本。

通过 Skill Installer 安装后，从同一提交补齐 sparse checkout 遗漏的子目录。
保留 SKILL.md、references、templates、校验/归档/看板脚本及看板 HTML 模板。
不引入上游 Git 元数据、宣传图片、演示数据、导入/导出素材工具和 npm 包配置。

本地差异：proposed 模板增加现有能力和影响范围；implemented 模板增加能力与影响说明。
其余引入文件保持该提交原文。上游更新必须先审阅 diff，不能自动覆盖项目适配。

项目 AGENTS.md 的授权、执行顺序和 Bun 命令优先于上游默认交互流程。
项目入口：`make verify-notes`；详见 `docs/AGENT_NOTES.md`。
