# Meta Pulse 工作流来源与适配

## 当前安装

2026-10-17 接入 [project-decision-kit v1.0.1](https://github.com/nanashiwang/new-api/releases/tag/project-decision-kit-v1.0.1)。完整 ZIP 的 SHA256：

```text
0c7d0a40c18040667d486eb01e21ea9e16907ac2177540a4c72bb067de2a17c8
```

首次预览仅发现既有 `UPSTREAM.md` 与包内通用说明不同，安装器拒绝写入。经逐字比较，Skill、参考文档、模板和工具源码均相同。项目原文完整保留在下方；工具目录接纳发布包原版来源说明，其引入经历描述的是分发包，不是 Meta Pulse 的安装历史。

`.agents/workflow-install.json` 记录原版工具及托管规则的哈希。项目适配保存在本文件、`docs/AGENT_NOTES.md`、原有 AGENTS 规则和 Shell/Make/CI 入口中，不改写安装器、不伪造清单，也不以删除冲突文件的方式绕过检查。后续升级仍先预览，有冲突先比较并保留自定义内容。

保留原有笔记树和能力地图，不引入来源项目的业务笔记。继续复用 `make verify-notes` 与独立 Verify Agent Notes CI，不另建重复工作流；具体命令和验证边界见 [使用说明](AGENT_NOTES.md)。

## 接入前的项目来源说明（完整原文）

下文是本次整合前的来源与适配记录。当前安装与入口以本页上文及使用说明为准。

# Provenance and local adaptation

Source: https://github.com/czm15053/write-notes-like-deepseek
Pinned upstream commit: `8f3b404cf9fd8102f96561f390219877abf094a8`.
Imported on 2026-09-29 from the reviewed project-local bundle in newapi commit `599fb93745755ae9953ae68e811b292a04f9f3b7`.
The upstream README declares MIT, but that upstream commit has no standalone LICENSE file; no license text is invented here.

The bundle contains the original Skill, reference documents, verification/archive/board scripts and the board HTML template. Upstream text is retained in its original language as third-party material. Promotional images, demo notes, Git metadata and asset-import/export utilities are excluded.

The proposed and implemented templates add an existing-capabilities/impact section. This repository replaces the bundle provenance text; other source files retain the reviewed bundle content. Updates require reviewing the source diff and preserving local policy rather than copying the latest version blindly.

Project AGENTS.md and user authorization override imported interaction defaults. The local entry point is `bash .agents/verify-notes.sh`; see `docs/AGENT_NOTES.md`. Bun is a development/CI tool only. No runtime package manager is replaced.
