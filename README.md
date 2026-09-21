<div align="center">

# H3 OneClick

**裸机 → 探测 → 选档 → 安装 → 出片。一键把任意 GPU 机器变成 H3 视频生成节点。**

![platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux%20%7C%20macOS-blue)
![go](https://img.shields.io/badge/go-1.25+-00ADD8?logo=go)
![release](https://img.shields.io/github/v/release/yoligehude14753/h3-oneclick)
![license](https://img.shields.io/badge/license-private-lightgrey)

<a href="docs/media/h3-oneclick-demo.mp4">
  <img src="docs/media/h3-oneclick-demo.gif" alt="H3 OneClick demo — scan → resolve → install → generate" width="100%">
</a>

*40 秒实录（北京 heyi · RTX 5090 D ×2）：扫描 → `turbo-4-768` 命中 → 资产复用/下载 → `READY_FOR_BASELINE` → ComfyUI 启动 → 末尾是真实生成的 mp4。点图看完整有声版。*

</div>

## 它做什么

给一个没有任何环境配置的机器，自动完成 H3 视频生成部署的全流程：

- **硬件探测**：NVIDIA GPU / Apple Silicon、显存、内存、磁盘、CUDA、ffmpeg
- **ComfyUI 发现**：识别已有实例的 venv / adapter / 模型存量，能复用就不重下
- **Profile 门控**：按平台 + 单卡空闲显存自动选档，不满足的直接阻断并给出原因
- **来源探测与镜像切换**：HF 主源超时自动切 `hf-mirror.com` / jsDelivr same-file mirror
- **真实安装**：ComfyUI venv、DiT/TE/VAE 权重、turbo LoRA、workflow、adapter 全部落盘 + SHA 校验 + 回执
- **端到端出片**：官方 subgraph workflow 自动转 API prompt，提交 `/prompt` 生成 mp4（h264 + AAC）

## 形态

| 入口 | 说明 |
|---|---|
| 桌面 GUI | Wails 原生窗口（`gui/`），四步控制台界面，复用同一引擎 |
| CLI + Web UI | 单文件服务 `:8765`，内嵌网页版同样流程 |

## 快速开始

```sh
# CLI + Web UI → http://127.0.0.1:8765
go run ./cmd/h3-oneclick

# 桌面 GUI（macOS，需 wails CLI）
cd gui && wails build   # → gui/build/bin/h3-oneclick.app
```

单文件构建：

```sh
GOOS=windows GOARCH=amd64 go build -o dist/h3-oneclick-windows-amd64.exe ./cmd/h3-oneclick
GOOS=linux   GOARCH=amd64 go build -o dist/h3-oneclick-linux-amd64   ./cmd/h3-oneclick
GOOS=darwin  GOARCH=arm64 go build -o dist/h3-oneclick-macos-arm64   ./cmd/h3-oneclick
```

## H3 Profiles

| Profile | 档位 | 栈 |
|---|---|---|
| `native-20` | 原生 20 步 | `cuda-native20-bf16` |
| `turbo-8` | Turbo 8 步 | `cuda-turbo8-bf16` |
| `turbo-4-768` | Turbo 4 步 768 宽 | `cuda-turbo4-768-bf16`（双 32G 卡实测推荐） |

## API

`/api/scan` · `/api/best-config` · `/api/plan` · `/api/sources/probe` · `/api/install` · `/api/template/open` · `/api/health`

均返回 JSON，失败含 `error`/`message`；`/api/scan` 支持显式 `paths`/`ports` 指定 ComfyUI 位置。

## 实机验收

| 机器 | 硬件 | 结果 |
|---|---|---|
| daheng（北京） | 2× RTX 5090 D | 全链路通过，`864×480 / 5.17s / h264+AAC` |
| heyi（北京） | RTX 5090 D + RTX 5090，1133G RAM | GUI 流程 + 出片通过；HF 主源不可达时镜像兜底生效 |
| MacBook M4 | Apple Silicon 24G | 探测 + 推荐 `macos-metal-h3c`（external profile，仅建议不安装） |

## 边界

- 权重不打包进启动器；manifest 记录精确路径与主源/镜像。
- 默认实例扫描覆盖 `~/ComfyUI`、`~/AI/ComfyUI` 及 CWD 下 `./ComfyUI`；其他位置需 `/api/scan` 显式传 `paths`（BUG-002）。
- Windows CUDA 全链路未实测。
- 架构台账：[docs/ARCHITECTURE_FEATURE_LEDGER.md](docs/ARCHITECTURE_FEATURE_LEDGER.md)
