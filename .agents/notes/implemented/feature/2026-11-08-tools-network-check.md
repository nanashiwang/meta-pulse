# Agent Note: 顶栏工具分组与网络自检

Status: implemented

## Problem

社区需要放置一些小型工具，首个是网络自检。工具不应占用社区、集市、模型榜这类主板块的权重，也不能依赖论坛、Pulse 或 Intel 是否可用。

## Existing capabilities and impact

- 顶栏主导航位于 [app.js](../../../../metar-frontend/production/src/app.js) 的 `topbar()`，并在 [native-shell.js](../../../../services/forum-plugin/user-center-pulse/native-shell.js) 中同步到 Answer 原生页头。手机端隐藏顶栏，使用侧栏抽屉。
- 页面路由由 [routes.json](../../../../metar-frontend/shared/routes.json) 生成前端、插件与 nginx 路由；功能脚本通过 `loadFeatureScript` 按需加载，参照集市与模型榜接入。
- 用户参考了 FuckClaude 项目与 ippure 的 Claude 检测页。二者都以规避 Claude 地区风控为目的（“中国用户”指纹打分、防封教程），本工具不采用这一定位。
- 检索 `.agents/notes` 无相关旧笔记，新建。

## Decision

- 顶栏在「社区 / 集市 / 模型榜 / Pulse」之后增加「工具」下拉分组，列出各工具与「全部工具」(`/tools`)；手机端在侧栏底部、Pulse 上方增加「工具」入口；Answer 原生页头增加 `/tools` 链接。
- 工具清单只在 `app.js` 的 `TOOLS` 中定义一次，顶栏菜单与 `/tools` 总览共用。
- `/tools/network` 由 [tools.js](../../../../metar-frontend/production/src/tools.js) 在浏览器内完成：出口 IP、协议、时区、UTC 偏移、语言偏好与日期格式如实展示，不打分；WebRTC 检测需用户点击，页面写明会连接 `stun.cloudflare.com`，同协议公网地址与出口不同时提示可能绕过代理或 VPN；DNS 泄露检测需要专用解析服务，暂不提供。
- 出口 IP 由 nginx `location = /metar/api/tools/ip` 直接返回 real_ip 之后的 `$remote_addr`，不经过 Answer、Pulse 或 Intel，不带 Cookie。
- 结果只在页面展示，不保存、不上传，也不产生 Contribution、Ticket 或 Reward。
- 页面布局（同日调整）：顶部说明卡含「开始检测」按钮、STUN 说明与三项状态胶囊概览；下方为出口 IP、WebRTC、浏览器环境三张检测卡，各自带状态胶囊；WebRTC 逐个地址标注「与出口一致 / 不同 / 内网 / mDNS 已隐藏」。出口 IP 大字显示并可复制；读取失败时显示原因（超时、被拦截、服务器状态码、响应无法识别）与重试按钮，替代原来笼统的「暂时无法读取」。
- 用户要求将该页命名为「Claude 检测」并参照 ippure 的 Claude 检测页；名称与定位保持通用「网络自检」，不以规避 Claude 地区限制或风控为目的，只借鉴状态卡片式布局。

## Alternatives considered

- **作为侧栏类别或顶栏一级链接** — 最强论据是可见度最高。否决：用户明确要求工具不占大板块权重；下拉分组可以继续容纳其他小工具。
- **移植 FuckClaude 的中国环境评分与防封指南** — 最强论据是现成、MIT 许可、需求明确。否决：其目的在于规避 Claude 地区限制与风控识别，不纳入社区功能；只保留通用的网络与隐私自检。
- **由 Intel 或论坛插件提供出口 IP / IP 归属** — 最强论据是可以扩展 ASN、地区等信息。否决：首版只需要出口地址，nginx 已有真实 IP，不必新增服务依赖或第三方密钥；IP 归属另行立项。

## Verification

- 新增 [tools.test.js](../../../../metar-frontend/production/tests/tools.test.js) 覆盖：地址分类、ICE 候选解析、泄露判定（同协议不同地址、映射 IPv6、另一协议、内网、mDNS、未知出口）、UTC 偏移、总览转义、页面不出现评分、出口请求 `credentials: 'omit'`/`no-store` 与非法响应拒绝、对端关闭与 STUN 配置。
- `make test-community` 中的路由生成检查、前端 node 测试、生产构建与插件测试；`deploy/test.sh` 的 nginx 静态检查。
- 出口请求失败原因（格式、HTML 响应、HTTP 状态、网络拦截、超时）与地址标注的单元测试。
- 生产 metarhub.com：直接打开与经顶栏菜单进入 `/tools/network` 均读到出口 IP，接口连续返回 200 JSON。
- 本地构建预览：桌面与 375px 手机宽度、深浅主题、WebRTC 检测流程；手机无横向溢出。改版后的页面尚未部署到生产。

## Consequences

- 新增工具只需追加 `TOOLS` 条目、页面实现、`routes.json` 路由并重新生成。
- WebRTC 检测会向第三方 STUN 服务暴露访客的公网地址，因此保持用户点击触发并在页面说明。
- 未来提供 DNS 泄露或 IP 归属需要独立服务或第三方数据，届时需单独评审隐私与依赖边界。
