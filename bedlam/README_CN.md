# Bedlam

Bedlam 是 Markku Rossi 的 MPC 引擎的一个分支，专为 Quilibrium 项目重新设计，并使用 FERRET 进行不经意传输（OT）。

## 项目概述

Bedlam 是一个多方计算（MPC）框架，允许多个参与方在不泄露各自私有输入的情况下共同计算函数。主要特点包括：

- 支持安全的两方和多方计算
- 使用 QCL (Quilibrium Circuit Language) 作为高级编程语言
- 提供从 QCL 到电路的编译器
- 实现了多种不经意传输（OT）协议
- 支持各种密码学原语（如 AES、SHA256、RSA 等）

## 主要组件

### 1. QCL 语言

QCL 是一种类似 Go 的高级语言，用于编写可以被编译成布尔电路的程序。QCL 支持：

- 基本数据类型（uint、uint64、uint256 等）
- 函数定义和调用
- 条件语句和循环
- 导入包和使用外部函数

### 2. 编译器

Bedlam 包含一个将 QCL 代码编译为布尔电路的编译器，支持多种输出格式：

- QCLC（Quilibrium 电路格式）
- Bristol 格式
- SSA（静态单赋值）汇编

### 3. 安全计算引擎

- garbled：实现了基于混淆电路的两方计算
- 支持多种不经意传输协议

## 编译和运行

### 环境准备

1. 安装 Go 1.18 或更高版本
2. 安装 Rust 和 Cargo（用于 FERRET 库）
3. 安装必要的依赖库（GMP、MPFR、FLINT）

### 编译 FERRET 库

```bash
# 在项目根目录下执行
cd ferret
./generate.sh
```

### 编译 Bedlam

```bash
# 在 bedlam 目录下执行
CGO_LDFLAGS="-L$ROOT_DIR/target/release -L/usr/local/lib/ -lstdc++ -lferret -ldl -lm -lcrypto -lssl" \
CGO_ENABLED=1 \
go build ./...
```

### 编译 QCL 文件

使用 `garbled` 工具编译 QCL 文件：

```bash
# 编译 QCL 文件为电路
./garbled -circ examples/add.qcl

# 编译为 SSA 汇编
./garbled -ssa examples/add.qcl

# 指定输出格式（qclc 或 bristol）
./garbled -circ -format=bristol examples/add.qcl
```

### 运行安全计算

安全计算需要两方参与（Garbler 和 Evaluator）：

1. 启动 Evaluator：

```bash
./garbled -e -i <evaluator-input> examples/add.qcl
```

2. 启动 Garbler：

```bash
./garbled -i <garbler-input> examples/add.qcl
```

例如，对于 add.qcl 示例：

```bash
# Evaluator
./garbled -e -i 0xb33d6a91b4ca8ac31c639c6742cba5a74c661a63311548af191c298a945d4891 examples/add.qcl

# Garbler
./garbled -i 0x5bf6db5927d799cf225f165e9508238edc5a1200fcad08c6411648733eb3100f examples/add.qcl
```

## 主要应用程序分析

### garbled/main.go

`garbled` 是 Bedlam 的主要应用程序，用于编译 QCL 文件和执行安全计算。主要功能：

1. 编译 QCL 文件为电路或 SSA 汇编
2. 作为 Garbler 或 Evaluator 执行安全计算
3. 支持不同的不经意传输协议
4. 提供网络通信功能，允许远程参与方进行计算

主要命令行参数：
- `-e`：以 Evaluator 模式运行（默认为 Garbler）
- `-circ`：编译 QCL 为电路
- `-ssa`：编译 QCL 为 SSA 汇编
- `-format`：指定电路输出格式
- `-i`：指定输入值
- `-v`：启用详细输出
- `-address`：指定网络地址和端口
- `-ot-address`：指定 OT 协议的地址和端口

### objdump/main.go

`objdump` 是一个用于查看编译后电路文件内容的工具。它可以解析和显示电路的结构、输入/输出、门电路等信息。

## QCL 示例分析

Bedlam 提供了多种 QCL 示例，展示了不同的功能：

1. **基本算术运算**：add.qcl、mult.qcl
2. **密码学操作**：aes.qcl、sha256.qcl、hmac-sha256.qcl
3. **公钥加密**：rsa.qcl、montgomery.qcl
4. **多方计算**：3party.qcl

## 使用 QCL 实现 ERC20 类似功能

QCL 理论上可以实现类似 ERC20 的基本功能，但有一些限制。以下是一个简化的 ERC20 实现示例：

```go
// erc20.qcl
package main

import (
    "math"
)

// 简化的 ERC20 合约
type ERC20 struct {
    // 总供应量
    totalSupply uint256
    // 余额映射 (只能表示有限数量的账户)
    balances [10]uint256
    // 授权映射 (简化版)
    allowances [10][10]uint256
    // 合约拥有者
    owner uint8
}

// 转账操作
func transfer(token ERC20, from uint8, to uint8, amount uint256) (ERC20, bool) {
    // 检查余额是否足够
    if token.balances[from] < amount {
        return token, false
    }
    
    // 执行转账
    token.balances[from] = token.balances[from] - amount
    token.balances[to] = token.balances[to] + amount
    
    return token, true
}

// 授权操作
func approve(token ERC20, owner uint8, spender uint8, amount uint256) ERC20 {
    token.allowances[owner][spender] = amount
    return token
}

// 授权转账
func transferFrom(token ERC20, spender uint8, from uint8, to uint8, amount uint256) (ERC20, bool) {
    // 检查授权额度
    if token.allowances[from][spender] < amount {
        return token, false
    }
    
    // 检查余额
    if token.balances[from] < amount {
        return token, false
    }
    
    // 更新授权额度
    token.allowances[from][spender] = token.allowances[from][spender] - amount
    
    // 执行转账
    token.balances[from] = token.balances[from] - amount
    token.balances[to] = token.balances[to] + amount
    
    return token, true
}

// 铸造新代币 (只有拥有者可以)
func mint(token ERC20, caller uint8, to uint8, amount uint256) (ERC20, bool) {
    if caller != token.owner {
        return token, false
    }
    
    token.balances[to] = token.balances[to] + amount
    token.totalSupply = token.totalSupply + amount
    
    return token, true
}

// 主函数 - 处理不同的操作类型
func main(token ERC20, operation uint8, caller uint8, target uint8, amount uint256, target2 uint8) (ERC20, bool) {
    // operation: 1=transfer, 2=approve, 3=transferFrom, 4=mint
    
    if operation == 1 {
        return transfer(token, caller, target, amount)
    } else if operation == 2 {
        newToken := approve(token, caller, target, amount)
        return newToken, true
    } else if operation == 3 {
        return transferFrom(token, caller, target, target2, amount)
    } else if operation == 4 {
        return mint(token, caller, target, amount)
    }
    
    return token, false
}
```

### 实现限制

虽然可以实现基本功能，但 QCL 实现 ERC20 有以下限制：

1. **状态存储**：QCL 不直接支持持久化状态存储，每次计算都需要提供完整状态
2. **地址映射**：无法像以太坊那样支持任意地址映射，只能使用固定大小数组
3. **事件**：QCL 不支持事件触发机制
4. **安全性**：需要额外机制确保状态一致性和防止重放攻击
5. **可扩展性**：MPC 计算开销较大，不适合高频交易场景

尽管有这些限制，QCL 仍然可以用于实现简化版的代币功能，特别适合需要隐私保护的场景。