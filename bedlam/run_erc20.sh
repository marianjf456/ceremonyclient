#!/bin/bash
set -euo pipefail

# 确定项目根目录
ROOT_DIR="$( cd "$(dirname "$(realpath "$( dirname "${BASH_SOURCE[0]}" )")")" >/dev/null 2>&1 && pwd )"
BEDLAM_DIR="$ROOT_DIR/bedlam"
BIN_DIR="$BEDLAM_DIR/bin"
QCLC_FILE="$BEDLAM_DIR/apps/garbled/examples/erc20.qclc"

# 设置 QCLDIR 环境变量
export QCLDIR="$BEDLAM_DIR"

# 检查参数
if [ "$#" -lt 1 ]; then
    echo "Usage: $0 <mode> [address]"
    echo "  mode: 'e' for Evaluator, 'g' for Garbler"
    echo "  address: optional, format 'host:port'"
    exit 1
fi

MODE="$1"
ADDRESS="${2:-}"

# 默认输入值
INPUT="token={totalSupply=1000,balances=[500,300,200,0,0,0,0,0,0,0],allowances=[[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0],[0,0,0,0,0,0,0,0,0,0]],owner=0},operation=1,caller=0,target=1,amount=100,target2=0"

# 准备命令
CMD="$BIN_DIR/garbled"
if [ "$MODE" = "e" ]; then
    CMD="$CMD -e"
fi

CMD="$CMD -i \"$INPUT\""

if [ -n "$ADDRESS" ]; then
    CMD="$CMD -address $ADDRESS"
fi

CMD="$CMD $QCLC_FILE"

echo "Executing: $CMD"
eval $CMD