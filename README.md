# H3 OneClick Launcher

这是 H3 OneClick 的可运行 MVP：Go 单文件服务、内置本地 Web UI、硬件/ComfyUI 探测、完整配置栈 resolver、主源→`hf-mirror.com` 同文件镜像切换、安装计划和回执。

## 运行

```sh
go run ./cmd/h3-oneclick
```

打开 `http://127.0.0.1:8765`。

常用参数：

```sh
go run ./cmd/h3-oneclick \
  -addr 127.0.0.1:8765 \
  -state-dir "$HOME/.local/share/h3-oneclick"
```

构建当前平台的单文件：

```sh
go build -o h3-oneclick ./cmd/h3-oneclick
```

仓库内已生成的交叉编译候选位于 `dist/`：Windows x64、Linux x64、macOS arm64。它们未签名/未公证，属于本地构建产物。

交叉编译候选：

```sh
GOOS=windows GOARCH=amd64 go build -o dist/h3-oneclick-windows-amd64.exe ./cmd/h3-oneclick
GOOS=linux GOARCH=amd64 go build -o dist/h3-oneclick-linux-amd64 ./cmd/h3-oneclick
GOOS=darwin GOARCH=arm64 go build -o dist/h3-oneclick-macos-arm64 ./cmd/h3-oneclick
```

## 当前边界

- 权重不打包进启动器；manifest 记录精确文件路径和主源/同文件镜像。
- `hf-mirror.com` 只作为同 repo/path 镜像；ModelScope/RunningHub 不同格式方案不会静默替换。
- 现有 ComfyUI 可被探测和复用；空机器勾选运行时引导后，会从官方 ComfyUI Git 仓库创建隔离 venv 并安装 `requirements.txt`。
- 模板打开会先确认 ComfyUI 健康，把官方 workflow 写入 ComfyUI `/userdata` 并安装 `H3OneClickAdapter`；新运行时可通过前端回调返回 `TEMPLATE_READY`，已运行实例需要重启加载扩展，否则保持 `TEMPLATE_PENDING`。
- 未在真实 Windows/Linux/Apple Silicon GPU 上完成发布验收；静态检查和 fixture 测试不等于跨平台实机通过。

2026-08-24 在北京 Heyi 做过隔离验证：发现现有 ComfyUI 0.3.43/18188；双 32GB GPU 各约 3.5GB 空闲时，resolver 按单卡门阻断 24GB 栈；官方 HF DiT 探测超时后切换到 `hf-mirror.com`；ComfyUI `/userdata` workflow 保存与 SHA 读回通过。没有停止现役容器，也没有宣称 GPU 生成通过。

## API

核心接口：`/api/scan`、`/api/best-config`、`/api/plan`、`/api/sources/probe`、`/api/install`、`/api/template/open`。接口返回 JSON，失败包含 `error`、`message` 和时间戳。
