#!/bin/bash
set -euo pipefail

# 确定项目根目录
ROOT_DIR="$( cd "$(dirname "$(realpath "$( dirname "${BASH_SOURCE[0]}" )")")" >/dev/null 2>&1 && pwd )"
BEDLAM_DIR="$ROOT_DIR/bedlam"
BIN_DIR="$BEDLAM_DIR/bin"

# 设置 QCLDIR 环境变量
export QCLDIR="$BEDLAM_DIR"

# 检查参数
if [ "$#" -lt 3 ]; then
    echo "Usage: $0 <qclc-file> <mode> <input-file> [address]"
    echo "  mode: 'e' for Evaluator, 'g' for Garbler"
    echo "  address: optional, format 'host:port'"
    exit 1
fi

QCLC_FILE="$1"
MODE="$2"
INPUT_FILE="$3"
ADDRESS="${4:-}"

# 如果提供的是相对路径，转换为绝对路径
if [[ ! "$QCLC_FILE" = /* ]]; then
    QCLC_FILE="$(pwd)/$QCLC_FILE"
fi

if [[ ! "$INPUT_FILE" = /* ]]; then
    INPUT_FILE="$(pwd)/$INPUT_FILE"
fi

# 检查文件是否存在
if [ ! -f "$QCLC_FILE" ]; then
    echo "Error: Circuit file '$QCLC_FILE' not found."
    exit 1
fi

if [ ! -f "$INPUT_FILE" ]; then
    echo "Error: Input file '$INPUT_FILE' not found."
    exit 1
fi

# 准备命令
CMD="$BIN_DIR/garbled"
if [ "$MODE" = "e" ]; then
    CMD="$CMD -e"
fi

CMD="$CMD -i $INPUT_FILE"

if [ -n "$ADDRESS" ]; then
    CMD="$CMD -address $ADDRESS"
fi

CMD="$CMD $QCLC_FILE"

echo "Executing: $CMD"
$CMD