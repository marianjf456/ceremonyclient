#!/bin/bash
set -euo pipefail

# 确定项目根目录
ROOT_DIR="$( cd "$(dirname "$(realpath "$( dirname "${BASH_SOURCE[0]}" )")")" >/dev/null 2>&1 && pwd )"
BEDLAM_DIR="$ROOT_DIR/bedlam"
BIN_DIR="$BEDLAM_DIR/bin"

# 创建 pkg 目录（如果不存在）
PKG_DIR="$BEDLAM_DIR/pkg"
mkdir -p "$PKG_DIR"

# 设置 QCLDIR 环境变量指向 bedlam 目录
export QCLDIR="$BEDLAM_DIR"

# 检查输入文件是否存在
if [ "$#" -lt 1 ]; then
    echo "Usage: $0 <qcl-file> [additional-garbled-args]"
    exit 1
fi

QCL_FILE="$1"
shift

# 如果提供的是相对路径，转换为绝对路径
if [[ ! "$QCL_FILE" = /* ]]; then
    QCL_FILE="$(pwd)/$QCL_FILE"
fi

# 检查文件是否存在
if [ ! -f "$QCL_FILE" ]; then
    echo "Error: File '$QCL_FILE' not found."
    exit 1
fi

echo "Running garbled with QCLDIR=$QCLDIR"
echo "QCL file: $QCL_FILE"
echo "Additional arguments: $@"

# 运行 garbled 工具
"$BIN_DIR/garbled" "$@" "$QCL_FILE"