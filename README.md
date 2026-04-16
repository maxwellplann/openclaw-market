# OpenClaw Market

一个基于 Go 的 OpenClaw AI 控制台示例项目，当前结构尽量对齐 1Panel 的 AI 模块：

- 用户注册、登录、退出
- 模型账号（Provider Account）
- 账号模型（Account Models）
- 智能体实例（Agents）
- 智能体模型配置（主模型 / fallback）
- 通过微信扫码页面连接 channel
- 创建智能体时通过 Docker `create` 创建一个独立 OpenClaw 容器

## 运行

```bash
cd openclaw-market
go run ./cmd/openclaw-market
```

默认监听 `:8080`，可通过 `OPENCLAW_MARKET_ADDR` 覆盖。

创建容器前需要先启动本机 Docker daemon。默认镜像为 `ghcr.io/openclaw/openclaw:latest`，该默认值与 1Panel 当前 OpenClaw 安装方式保持一致；也可通过 `OPENCLAW_AGENT_IMAGE` 覆盖。

## 设计说明

- 结构参考 `1Panel` AI 模块：`AgentAccount`、`AgentAccountModel`、`Agent`、`Agent model config`。
- 每次创建智能体都会对应一个独立的 Docker 容器。
- 当前微信扫码为模拟接入流程，后续可把 `/bindings/{token}` 的确认逻辑替换为真实微信 webhook / 回调处理。
- 数据保存在 `data/store.json`，容器挂载目录位于 `data/openclaws/<container-name>/`，适合原型验证与本地演示。
