#!/bin/bash
set -euxo pipefail

# This script builds the bedlam binaries and properly links with the FERRET library

# 确定项目根目录
ROOT_DIR="$( cd "$(dirname "$(realpath "$( dirname "${BASH_SOURCE[0]}" )")")" >/dev/null 2>&1 && pwd )"

BEDLAM_DIR="$ROOT_DIR/bedlam"
BINARIES_DIR="$ROOT_DIR/target/release"
OUTPUT_DIR="$BEDLAM_DIR/bin"

# 创建输出目录
mkdir -p "$OUTPUT_DIR"

pushd "$BEDLAM_DIR" > /dev/null

export CGO_ENABLED=1

os_type="$(uname)"
case "$os_type" in
    "Darwin")
        # Check if the architecture is ARM
        if [[ "$(uname -m)" == "arm64" ]]; then
            # MacOS ld doesn't support -Bstatic and -Bdynamic, so it's important that there is only a static version of the library
            export CGO_LDFLAGS="-L$BINARIES_DIR -L/usr/local/lib/ -L/opt/homebrew/Cellar/openssl@3/3.4.1/lib -lstdc++ -lferret -ldl -lm -lcrypto -lssl"
            
            # 构建各个应用程序并将它们放在 bin 目录中
            echo "Building garbled..."
            go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/garbled" ./apps/garbled
            
            echo "Building circuit..."
            go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/circuit" ./apps/circuit
            
            echo "Building objdump..."
            go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/objdump" ./apps/objdump
            
            echo "Building iotest..."
            go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/iotest" ./apps/iotest
            
            echo "Building iter..."
            go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/iter" ./apps/iter
            
            # 如果有其他应用程序，可以继续添加
        else
            echo "Unsupported platform"
            exit 1
        fi
        ;;
    "Linux")
        export CGO_LDFLAGS="-L/usr/local/lib -ldl -lm -L$BINARIES_DIR -lstdc++ -lcrypto -lssl -lferret -static"
        
        # 构建各个应用程序并将它们放在 bin 目录中
        echo "Building garbled..."
        go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/garbled" ./apps/garbled
        
        echo "Building circuit..."
        go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/circuit" ./apps/circuit
        
        echo "Building objdump..."
        go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/objdump" ./apps/objdump
        
        echo "Building iotest..."
        go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/iotest" ./apps/iotest
        
        echo "Building iter..."
        go build -ldflags "-linkmode 'external'" -o "$OUTPUT_DIR/iter" ./apps/iter
        ;;
    *)
        echo "Unsupported platform"
        exit 1
        ;;
esac

echo "Build completed. Binaries are located in $OUTPUT_DIR"
popd > /dev/null
