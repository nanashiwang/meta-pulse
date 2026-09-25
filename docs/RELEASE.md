# 正式版本发布与部署

版本事实源为根目录 `VERSION`（例如 `0.1.1`），对应不可覆盖的 Git 标签 `v0.1.1`。中文更新说明保存在 `releases/v0.1.1.md`，总览同步 `CHANGELOG.md`。

## 发布

1. 更新版本与中文说明，完成改动相关测试，提交并推送 `main`。
2. 先对已提交内容运行检查（不读取未提交版本文件），通过后才创建附注标签并推送：

   ```bash
   python3 release/preflight.py v0.1.1
   git tag -a v0.1.1 -m '发布 Meta Pulse v0.1.1'
   git push origin v0.1.1
   ```

3. `Release` 工作流首先检查标签指向当前提交、VERSION 与标签一致、对应中文说明标题及 CHANGELOG 条目完整；失败时不会启动完整 CI 或构建。普通 CI 同样检查已提交版本元数据并执行发布回归测试。构建和上传脚本重复执行检查，已公开版本（包括缺少附件的版本）必须使用下一个可用补丁版本，不能移动标签或覆盖附件。随后工作流复用完整 CI（Go、MySQL、Forum/Pulse 镜像和博客）。全部通过后，按标签提交隔离构建附件：

   - `meta-pulse_v0.1.1_linux_amd64.tar.gz`
   - `meta-pulse_v0.1.1_linux_arm64.tar.gz`
   - `meta-pulse_v0.1.1_web.tar.gz`：博客及 `metar/` 正式前端。
   - `meta-pulse_v0.1.1_source.tar.gz`：受 Git 跟踪的源码。
   - `release-manifest.json`：版本、完整提交、附件名称、大小与 SHA-256。
   - `SHA256SUMS`：四个包与清单的校验值。

4. 工作流先上传草稿，再重新下载全部附件核对文件集合与 SHA-256，最后发布正式 Release。已发布版本禁止覆盖，修改须升版本；失败的草稿可以重跑。Release 不持有生产 SSH 凭据、不自动改服务器配置。

二进制包包含 API、Worker、运维工具，均支持 `--version`，无需配置或数据库即可查询。源码从 `git archive` 取得，不带本机未跟踪文件、`.env`、Compose 覆盖配置、数据库或私钥卷；静态资源同样从隔离源码构建。包内路径会检查，发现运行配置或私钥目录则终止发布。本版容器从源码构建，不单独发布公共镜像。

下载后可使用 `sha256sum --check SHA256SUMS` 核验。二进制包用于自行管理 Linux 服务，现有 Docker 部署按下述流程升级。

## 已有服务器部署

```bash
cd /opt/meta-pulse
metar update --release v0.1.1
```

旧部署尚未包含 `--release`，或正从 v0.1.0 升级时，先确认工作区无受跟踪改动，再按已发布标签取得修复后的脚本：

```bash
git fetch origin refs/tags/v0.1.1:refs/tags/v0.1.1
git merge --ff-only v0.1.1
./deploy/update.sh --release v0.1.1
```

版本部署要求发布页已有清单且提交与标签一致，只允许 Git 快进；当前代码超前、分叉、标签移动或版本号不匹配时停止，不对生产仓库执行强制 reset。要继续跟随开发分支，可显式使用现有 `--ref main` 流程。

部署沿用更新锁，先备份两套数据库、`.env`、Compose 配置与 API/Worker 私钥卷，再构建全套服务与静态资源、排空旧 API、前进迁移、重建容器并检查健康。博客目录或 Nginx 配置被替换时重建网关，避免 Docker 仍挂载旧 inode；同时在网关容器内核验博客与 METAR 首页文件。正式版本不支持跳过组件或构建。结尾核对 API、Worker、Forum 镜像的版本与完整提交，成功后写入 `.data/deployed-release.txt`。不修改已有配置、绑定关系、限额、影子模式或发奖开关。

## 验收与恢复

核对 Git 标签、GitHub Release 的全部附件、工作流结果与三个运行容器的 `org.opencontainers.image.version` / `revision`；随后验证 METAR 管理页、new-api 摘要与运营概览。部署成功不代表真实发奖已开放。

失败时保留旧备份、数据库和私钥卷，不自动执行数据库 Down，也不轮换密钥。先评估迁移兼容性和已产生的业务状态，再决定恢复镜像或在维护窗口恢复数据。不可只恢复数据库而遗漏对应的运行密钥卷。
