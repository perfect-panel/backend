# 文档导航

本目录是服务端唯一的文档目录，按读者分为三类。项目简介见根目录
[README.md](../README.md)（英文）与 [README_ZH.md](../README_ZH.md)（中文）。

## 使用指南（部署与运维）

| 文档 | 说明 |
|---|---|
| [guide/install.md](guide/install.md) / [install-zh.md](guide/install-zh.md) | 安装与部署（英文 / 中文） |
| [guide/config.md](guide/config.md) / [config-zh.md](guide/config-zh.md) | 配置项说明（英文 / 中文） |
| [guide/epay.md](guide/epay.md) / [epay-zh.md](guide/epay-zh.md) | EPay V1/V2 配置、收款、协议能力及升级说明（英文 / 中文） |
| [guide/api-changes-2026-09.md](guide/api-changes-2026-09.md) / [api-changes-2026-09-zh.md](guide/api-changes-2026-09-zh.md) | 2026 年 9 月审计修复带来的 API 变化清单（英文 / 中文），供前端与客户端维护者查阅 |

## 贡献指南

| 文档 | 说明 |
|---|---|
| [contributing/CONTRIBUTING.md](contributing/CONTRIBUTING.md) | 如何参与开发（英文） |
| [contributing/CONTRIBUTING_ZH.md](contributing/CONTRIBUTING_ZH.md) | 如何参与开发（中文） |

## 设计文档（面向开发者）

架构决策与功能设计。修改模块边界或目录结构时，先阅读 ADR-001 并同步更新。

| 文档 | 说明 |
|---|---|
| [design/adr-001-modular-monolith.md](design/adr-001-modular-monolith.md) | 模块化单体架构决策；`internal/arch` 测试据此强制边界 |
| [design/package-layout.md](design/package-layout.md) | 包结构与职责归属 |
| [design/device-authentication.md](design/device-authentication.md) | 设备登录的签名信封协议 |
| [design/node-device-admission.md](design/node-device-admission.md) | 节点设备准入、共享用户索引、过载行为与压测 |
| [design/edge-subscribe.md](design/edge-subscribe.md) | 边缘节点订阅下发 |
| [design/telegram-bot.md](design/telegram-bot.md) | Telegram Bot 集成 |
| [design/v2-order-checkout-design.md](design/v2-order-checkout-design.md) | 订单 outbox 与对账设计 |

## 约定

- 新的设计文档放入 `design/`，命名为小写中划线；正式的架构决策使用 `adr-NNN-` 前缀编号。
- 中英对照的用户文档放在同一目录，英文不加后缀，中文加 `-zh` 后缀（如 `config.md` / `config-zh.md`）。
- 文档内引用本仓库其他文件时使用相对于本文件的路径；根目录 README 为英文默认版，
  中文版位于 `README_ZH.md`，两者通过语言切换链接互指。
- 构建产物（如 `ppanel.json`）不放入本目录；Swagger 文档由 CI 同步到前端文档仓库。
