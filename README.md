# H3 OneClick

裸机 → 探测 → 选档 → 安装 → **出片**。

面向任意一台 GPU 机器的一键部署工具：自动识别硬件、发现已有 ComfyUI、按显存/平台门控选择 H3 profile、下载或复用模型/LoRA/workflow/插件、启动运行时并产出部署回执——最终真实生成 H3 视频。

## Demo

<video src="docs/media/h3-oneclick-demo.mp4" controls muted></video>

40 秒实录（北京 heyi · RTX 5090 D ×2）：环境扫描 → `turbo-4-768` 命中 → 资产复用/下载 → `READY_FOR_BASELINE` → ComfyUI 启动 + workflow 写入 → **末尾为安装完成后真实生成的 mp4（原始输出，未剪辑）**。

## 形态

| 入口 | 说明 |
|---|---|
| **桌面 GUI** | Wails v2 原生窗口（`gui/`），四步控制台界面，复用同一引擎 |
| **CLI + Web UI** | `h3-oneclick` 单文件服务（`:8765`），内嵌网页版同样流程 |

## 能力

- **硬件探测**：NVIDIA GPU / Apple Silicon（MPS）、显存、内存、磁盘、ffmpeg、CUDA
- **ComfyUI 发现**：扫描本机已有实例，识别 venv/adapter/模型存量，资产可复用则不重下
- **Profile 门控**：按平台 + 单卡空闲显存自动选档；不满足条件的 profile 直接阻断并给出原因
- **来源探测与镜像切换**：HF 主源超时/不可达自动切 `hf-mirror.com` / jsDelivr same-file mirror；不同格式源不静默替换
- **真实安装**：ComfyUI venv 创建或复用、模型/LoRA/workflow/adapter 落盘、SHA 校验、回执写 `state/receipts/`
- **端到端出片**：官方 subgraph workflow → API prompt 转换（`scripts/convert_h3_prompt.py`），`/prompt` 提交生成真实 mp4（h264 + AAC 音轨）

## H3 Profiles

| Profile | 档位 | 栈 |
|---|---|---|
| `native-20` | 原生 20 步 | `cuda-native20-bf16` |
| `turbo-8` | Turbo 8 步 | `cuda-turbo8-bf16` |
| `turbo-4-768` | Turbo 4 步 768 宽 | `cuda-turbo4-768-bf16`（实测双 32G 卡推荐此项） |

关键资产：DiT pruned-int8 权重、nvfp4 TE、video/audio VAE、turbo LoRA、官方 T2V workflow。

## 运行

```sh
# CLI + Web UI（打开 http://127.0.0.1:8765）
go run ./cmd/h3-oneclick

# 桌面 GUI（macOS，需 wails CLI）
cd gui && wails build   # → gui/build/bin/h3-oneclick.app
```

交叉编译 CLI：

```sh
GOOS=windows GOARCH=amd64 go build -o dist/h3-oneclick-windows-amd64.exe ./cmd/h3-oneclick
GOOS=linux   GOARCH=amd64 go build -o dist/h3-oneclick-linux-amd64   ./cmd/h3-oneclick
GOOS=darwin  GOARCH=arm64 go build -o dist/h3-oneclick-macos-arm64   ./cmd/h3-oneclick
```

## API

`/api/scan` · `/api/best-config` · `/api/plan` · `/api/sources/probe` · `/api/install` · `/api/template/open` · `/api/health` — 均返回 JSON，失败含 `error`/`message`。`/api/scan` 支持显式 `paths`/`ports` 指定 ComfyUI 位置。

## 实机验收记录

| 机器 | 硬件 | 结果 |
|---|---|---|
| daheng（北京） | 2× RTX 5090 D | 全链路通过，`MiniMax_H3_00001_.mp4` 864×480 / 5.17s / h264+AAC |
| heyi（北京） | RTX 5090 D + RTX 5090，1133G RAM | GUI 流程 + 出片通过；HF 主源不可达时镜像兜底生效 |
| MacBook M4 | Apple Silicon 24G | 探测 + 推荐 `macos-metal-h3c`（external profile，仅建议不安装） |

## 当前边界

- 权重不打包进启动器；manifest 记录精确路径与主源/镜像。
- 默认实例扫描覆盖 `~/ComfyUI`、`~/AI/ComfyUI` 及 CWD 下 `./ComfyUI`；其他位置需 `/api/scan` 显式传 `paths`（BUG-002）。
- Windows CUDA 全链路未实测。
- 架构台账见 [docs/ARCHITECTURE_FEATURE_LEDGER.md](docs/ARCHITECTURE_FEATURE_LEDGER.md)。
