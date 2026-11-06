#!/usr/bin/env bash
# 把 orch 安装到 PATH 里，并检查最基本的模型配置。
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

case "$(uname -s)" in
  Darwin)
    case "$(uname -m)" in
      arm64) BIN="orch-darwin-arm64" ;;
      x86_64)
        echo "Intel Mac 暂未提供预编译包，请在有 Go 环境的机器上执行："
        echo "  cd <axon 仓库> && GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -o orch ./cmd/orch"
        exit 1
        ;;
      *) echo "未知的 Mac 架构: $(uname -m)"; exit 1 ;;
    esac
    ;;
  Linux)
    BIN="orch-linux-amd64"
    ;;
  *)
    echo "暂不支持的系统: $(uname -s)"
    exit 1
    ;;
esac

if [[ ! -f "$HERE/$BIN" ]]; then
  echo "找不到 $HERE/$BIN —— 请确认解压完整"
  exit 1
fi

INSTALL_DIR="${ORCH_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$INSTALL_DIR"
install -m 755 "$HERE/$BIN" "$INSTALL_DIR/orch"

echo "已安装: $INSTALL_DIR/orch"

case ":$PATH:" in
  *":$INSTALL_DIR:"*)
    ;;
  *)
    echo
    echo "注意: $INSTALL_DIR 不在 PATH 里。把它加进去，例如："
    echo "  echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc && source ~/.zshrc"
    ;;
esac

echo
echo "下一步 —— 配好模型（三行，任选一家）："
echo "  export ORCH_API_KEY=sk-xxxx"
echo "  export ORCH_BASE_URL=https://api.deepseek.com"
echo "  export ORCH_MODEL=deepseek-chat"
echo
echo "然后："
echo "  orch do \"给订单接口加个超时重试\""
