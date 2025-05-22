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

使用 `garbled` 工具来编译 QCL 文件。该工具可以将 QCL 代码转换为多种格式，包括布尔电路或 SSA（静态单赋值）汇编。编译后的电路随后可用于安全计算。

**基本命令格式:**
```bash
./garbled [编译标志] <qcl_源文件路径>
```
通常，编译后的输出（无论是电路还是 SSA）会发送到标准输出 (stdout)，因此您需要使用重定向将其保存到文件中。

**主要编译标志:**

*   `-circ`: 将 QCL 文件编译成电路。这是生成可执行电路以进行后续安全计算的常用标志。
*   `-ssa`: 将 QCL 文件编译成 SSA 汇编代码。如果与 `-circ` 一起使用，会同时生成 SSA 和电路；如果单独使用 (`./garbled -ssa ...`)，则仅生成 SSA 输出，并且可能跳过完整的电路编译步骤。SSA 可以用于调试或进一步的电路分析。
*   `-format=<格式>`: 当使用 `-circ` 标志时，此标志用于指定输出电路的格式。
    *   `qclc`: (默认格式) Quilibrium 电路格式。这是 Bedlam 系统内部使用的主要格式。
    *   `bristol`: Bristol Fashion 格式，一种广泛用于学术界和各种 MPC 工具的通用电路表示格式。
*   `-O=<优化级别>`: 设置编译时的优化级别。例如, `-O=1` (默认值) 会启用诸如移除未使用门（门剪枝）之类的优化。更高级别的优化可能会在未来引入。目前，主要使用的级别是 `0` (无优化) 和 `1`。
*   `-i <输入类型列表>`: 为编译方（通常是 Garbler）指定输入的类型和数量。参数是一个逗号分隔的字符串，例如 `-i "uint64,uint32[8],bool"` 表示一个 `uint64` 类型的输入，一个包含8个 `uint32` 元素的数组，以及一个布尔值。这有助于编译器正确构建电路的输入部分，特别是对于期望特定输入结构的程序。
*   `-pi <对方输入类型列表>`: 为对方（在两方计算中，通常是 Evaluator）指定输入的类型和数量，格式同 `-i`。例如，`-pi "uint256"`。这个标志对于确保电路为所有参与方的输入正确配置至关重要。

**编译示例:**

1.  **编译为默认的 QCL 电路格式 (`.qclc`)**:
    ```bash
    ./garbled -circ examples/add.qcl > examples/add.qclc
    ```
    这将编译 `examples/add.qcl` 文件，并将生成的 `qclc` 格式电路保存到 `examples/add.qclc` 文件中。

2.  **编译为 Bristol 格式**:
    ```bash
    ./garbled -circ -format=bristol examples/add.qcl > examples/add.bristol
    ```
    这将编译 `examples/add.qcl` 文件，并将生成的 Bristol 格式电路保存到 `examples/add.bristol` 文件中。

3.  **编译为 SSA 汇编**:
    ```bash
    ./garbled -ssa examples/add.qcl > examples/add.ssa
    ```
    这将编译 `examples/add.qcl` 文件，并将生成的 SSA 汇编代码保存到 `examples/add.ssa` 文件中。

4.  **编译时指定输入大小 (例如，编译方提供一个 `uint64`，另一方也提供一个 `uint64`)**:
    ```bash
    ./garbled -circ -i "uint64" -pi "uint64" examples/sum.qcl > examples/sum.qclc
    ```
    假设 `examples/sum.qcl` 是一个需要两方各输入一个 `uint64` 数值的 QCL 程序。此命令会根据这些输入规范编译电路。

5.  **编译为 SSA 并启用优化**:
    ```bash
    ./garbled -ssa -O=1 examples/complex_logic.qcl > examples/complex_logic.ssa
    ```

这些标志和示例提供了使用 `garbled` 编译 QCL 文件的基础。根据具体的 QCL 程序和安全计算的需求，您可能需要组合使用这些标志。

### 运行安全计算

使用 `garbled` 工具执行编译后的 QCL 电路。这通常涉及两方：**Garbler (混淆方)** 和 **Evaluator (评估方)**。这是安全两方计算 (Secure Two-Party Computation, 2PC) 的一个典型实现。

*   **Garbler (混淆方)**: 负责准备混淆电路 (Garbled Circuit)，处理其自身的输入，并将混淆电路发送给评估方。
*   **Evaluator (评估方)**: 接收混淆电路，并结合其自身的输入来计算最终结果。评估方无法得知混淆方的原始输入，混淆方也无法得知评估方的原始输入（除非电路本身设计如此）。

**主要执行标志:**

*   `-e`: 以 Evaluator (评估方) 模式运行。如果省略此标志，`garbled` 默认以 Garbler (混淆方) 模式运行。
*   `-i <输入列表>`: 为当前参与方（Garbler 或 Evaluator）提供其私有输入值。
    *   输入列表是一个逗号分隔的字符串，其中每个值通常以十六进制表示 (例如 `0xdeadbeef`)。
    *   这些输入必须与编译电路时（通过 `-i` 和 `-pi` 标志为编译器指定的）该方预期的类型、数量和顺序完全匹配。例如，如果电路期望一个 `uint64` 和一个 `uint32`，则输入应按此顺序提供。
*   `-address=<IP地址:端口>`: 指定 Garbler 和 Evaluator 之间主通信通道的网络地址。Evaluator 在此地址和端口上监听传入连接，Garbler 连接到此地址和端口。如果未指定，通常默认为 `127.0.0.1:8080`。
*   `-ot-address=<IP地址:端口>`: 指定不经意传输 (Oblivious Transfer, OT) 协议通信的网络地址。OT 是许多安全计算协议（包括 Bedlam 使用的协议）中的一个关键子协议，由 FERRET 库处理。Evaluator 在此监听，Garbler 连接至此。如果未指定，通常默认为 `127.0.0.1:5555`。
*   `-stream`: (可选标志) 启用流式处理模式。这对于处理非常大的电路或在内存受限的环境中进行计算可能很有用，因为它允许数据分块处理，从而减少内存峰值。
*   `-v`: 启用详细输出 (verbose mode)。这会打印更多关于执行流程、通信和内部状态的调试信息，有助于诊断问题或更深入地理解计算过程。

**执行步骤示例 (以 `examples/add.qcl` 为例):**

安全计算通常需要按以下顺序启动两方。请注意，您应该使用先前编译好的电路文件 (例如 `examples/add.qclc`) 而不是原始的 `.qcl` 文件进行执行。

1.  **启动 Evaluator (评估方)**:
    Evaluator 必须首先启动，以便开始在指定的网络地址上监听来自 Garbler 的连接。

    ```bash
    ./garbled -e -i <评估方输入值> -address=127.0.0.1:8080 -ot-address=127.0.0.1:5555 examples/add.qclc
    ```
    例如，如果 `examples/add.qclc` 期望评估方提供一个256位整数，其值为 `0xb33d6a91b4ca8ac31c639c6742cba5a74c661a63311548af191c298a945d4891`:
    ```bash
    ./garbled -e -i 0xb33d6a91b4ca8ac31c639c6742cba5a74c661a63311548af191c298a945d4891 -address=127.0.0.1:8080 -ot-address=127.0.0.1:5555 examples/add.qclc
    ```
    启动后，Evaluator 将等待 Garbler 连接。

2.  **启动 Garbler (混淆方)**:
    Evaluator 开始监听后，Garbler 可以启动并连接到 Evaluator 正在监听的地址。

    ```bash
    ./garbled -i <混淆方输入值> -address=127.0.0.1:8080 -ot-address=127.0.0.1:5555 examples/add.qclc
    ```
    例如，如果 `examples/add.qclc` 期望混淆方提供一个256位整数，其值为 `0x5bf6db5927d799cf225f165e9508238edc5a1200fcad08c6411648733eb3100f`:
    ```bash
    ./garbled -i 0x5bf6db5927d799cf225f165e9508238edc5a1200fcad08c6411648733eb3100f -address=127.0.0.1:8080 -ot-address=127.0.0.1:5555 examples/add.qclc
    ```
    一旦 Garbler 连接成功，双方将执行安全计算协议。计算完成后，双方通常都会在其控制台得到输出结果（具体输出内容取决于 QCL 程序的设计）。

**关于多方计算 (MPC):**

虽然 `garbled` 工具主要演示了上述的两方计算 (2PC) 流程，但 QCL 语言本身支持更广泛的多方计算 (MPC) 概念。在 Quilibrium 的相关文档中（例如 "Running Applications"），可能会提到 "rendezvous request" 之类的机制。这通常指的是一种用于协调多个参与者（两个以上）加入同一个计算会话的系统。`garbled` 工具的当前用法更侧重于两个预先知道对方网络地址的参与者之间的直接交互。对于更复杂的多方场景，可能需要额外的协调服务或更高级的协议实现。

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

### apps/circuit/main.go

`apps/circuit/main.go` 程序是一个实用工具，旨在帮助可视化已编译电路文件的结构。它接收一个电路文件（通常为 QCLC 格式，由 `garbled -circ ...` 生成）作为输入，并以 DOT 格式生成电路的描述。然后，此 DOT 格式输出可与 Graphviz 工具（例如 `dot -Tsvg mycircuit.dot -o mycircuit.svg`）一起使用，以创建电路的可视化图表，例如 SVG 或 PNG 图像。

**使用方法：**

通常，您可以通过指定已编译电路文件的路径来运行它：

```bash
go run bedlam/apps/circuit/main.go <已编译电路文件的路径>
```

例如：
```bash
# 首先，将 QCL 文件编译为电路
./garbled -circ examples/add.qcl > add.qclc

# 然后，生成 DOT 表示
go run bedlam/apps/circuit/main.go add.qclc > add.dot

# 最后，使用 Graphviz 将 DOT 文件转换为 SVG 图像
dot -Tsvg add.dot -o add.svg
```
这将生成一个 SVG 图像 `add.svg`，显示 `add.qclc` 电路的视觉结构。

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

虽然理论上可以使用 QCL 实现类似 ERC20 的基本代币功能，但在实践中存在一些关键的限制，这些限制源于 QCL 作为电路编译语言的设计及其在 Quilibrium 整体架构中的角色：

1.  **状态存储与数据大小限制**: QCL 本身作为一种电路构建语言，不直接提供持久化状态存储机制。在实际的 Quilibrium 应用中，诸如用户余额之类的状态数据，通常会通过 RDF (Resource Description Framework) 定义的数据结构在底层的 Hypergraph 中进行管理和持久化。QCL 合约则专注于定义状态转换的隐私计算逻辑。至关重要的是，所有在 QCL 电路中处理的数据结构，包括用于存储余额 (`balances`) 和授权 (`allowances`) 的数组，都**必须具有预定义的、固定的最大容量**。例如，示例中的 `balances [10]uint256` 将合约能够直接管理的账户数量严格限制为10个。这与以太坊等平台上常见的动态调整大小的映射 (mapping) 数据结构形成鲜明对比，并对代币合约的设计（例如，可以支持的最大代币持有者数量）产生直接影响。若要支持更多用户或更复杂的状态表示，可能需要设计更复杂的数据结构（如基于固定大小数组的链式列表或树，前提是 QCL 对指针和内存控制的支持足够支撑此类模式）或在应用层面采用分片等策略。

2.  **地址与账户管理**: 如上所述，QCL 不支持类似以太坊的动态地址到值的映射。账户（或其等价物）必须通过固定大小的数组索引来间接表示。这意味着在设计 QCL 合约时，必须预先确定可以处理的最大账户数量。将外部用户身份映射到这些固定索引需要应用层面的额外逻辑。

3.  **事件通知**: QCL 本身不包含事件触发或日志记录机制。智能合约通常依赖事件向外部世界通知状态变化，但在 QCL 的 MPC 模型中，计算结果直接返回给参与方，任何进一步的通知或记录都需要在应用层面实现。

4.  **安全性考量**: 除了标准的智能合约安全实践外，基于 MPC 的 QCL 合约还需要考虑额外的安全层面。例如，需要依赖 Quilibrium 网络提供的机制来确保状态在多次计算之间的一致性、防止针对 MPC 协议的特定攻击（如重放攻击），以及保证参与方输入的有效性。

5.  **计算开销与可扩展性**: 多方计算 (MPC) 本质上比传统的非隐私计算具有更高的计算和通信开销。虽然 QCL 和 Bedlam 旨在优化这些过程，但对于需要高吞吐量或极低延迟的场景（如高频交易），直接在 MPC 中执行复杂逻辑可能不切实际。因此，QCL 合约更适合那些隐私保护需求优先于极致性能的场景。

6.  **公开可验证性 vs. 计算保密性**: QCL 的核心优势在于其能够对私有输入执行保密计算。这意味着计算过程和中间状态对外部观察者是不透明的。虽然可以实现 ERC20 的核心逻辑（如转账、余额查询），但如何使其状态变更（如代币余额的更新）像在以太坊等透明区块链上那样具有公开可验证性，则是一个独立的架构问题。这通常涉及到 QCL 应用如何与 Quilibrium 网络的共识机制和公共状态层交互，以选择性地公开或证明某些计算结果，同时保持输入的隐私性。QCL 本身专注于“如何计算”，而“计算结果如何被信任地公开”则依赖于更广泛的系统设计。

尽管存在这些限制，QCL 仍然是为特定类型的应用（尤其是那些需要强大隐私保护的金融或数据处理应用）实现定制化、安全计算逻辑的强大工具。通过理解这些限制，开发者可以更有效地设计其 QCL 程序和与之交互的 Quilibrium 应用。