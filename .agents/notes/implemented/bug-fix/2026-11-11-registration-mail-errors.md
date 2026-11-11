# Agent Note: 注册邮件失败反馈

Status: implemented

## Problem

Answer 1.7.1 注册、重发与后台测试邮件异步发送，SMTP 缺配置或拒收后页面仍显示成功。

## Existing capabilities and impact

复用 [Answer 构建补丁入口](../../../../services/forum/Dockerfile)、原生注册和激活 API、原生错误弹窗。检索现有笔记：Turnstile 与注册入口部分重叠但验证码策略不变；管理员免邮箱创建、域名迁移与本次发送反馈无冲突。不改 new-api 邮件服务。

## Decision

同步返回注册、重发、后台测试的邮件提交结果；错误使用稳定原因和本地化提示。注册发信失败保留待激活账号，明确指引登录后重发，不删除账号或放宽验证。SMTP 操作设总截止时间，短于浏览器请求超时。其他通知继续异步调用现有服务。

## Alternatives considered

- 只改页面文案：成本低，但仍不知道实际发送失败，不能满足需求。
- 发信失败删除账号：可直接重填注册，但发送结果可能不确定且账号存在并发关联，不安全。
- 持久化队列：能自动重试，但需要任务状态、存储和后台查询，超出此次错误反馈范围。

## Verification

Go 回归通过：缺配置、配置读取失败、缓存失败、SMTP DATA 拒收和接受、连接截止时间、TLS 不降级与 LOGIN 凭据保护。按 Dockerfile 顺序零 fuzz 叠加全部补丁后，export/content/siteinfo/user_admin 编译和邮件测试通过。实际请求拦截器执行验证两类 503 均弹出失败提示且不进入全页错误。

## Consequences

SMTP 接受不代表最终入箱；截止时间附近仍可能存在投递结果不确定。上线需重新构建 forum 并升级语言资源，本地修改不等于生产部署。

项目笔记校验在独立 LF 检出目录执行 `python .agents/workflow.py verify` 通过；原 Windows 工作区托管文件仅因 CRLF 换行导致哈希差异，不修改托管文件或安装清单。未执行生产部署或完整 forum 镜像构建。
