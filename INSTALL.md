# 安装说明

## 通过 go install 安装

### 前置条件

由于程序依赖 CGO 库，需要先安装以下依赖：

#### 1. SQLite3 开发库

**Ubuntu/Debian:**
```bash
sudo apt-get update
sudo apt-get install -y libsqlite3-dev
```

**CentOS/RHEL:**
```bash
sudo yum install -y sqlite-devel
# 或者对于较新版本
sudo dnf install -y sqlite-devel
```

**macOS:**
```bash
# 通常已包含，如果没有：
brew install sqlite
```

#### 2. DRMAA 库（如果使用 qsubsge 模式）

**方法 A: 通过包管理器安装（推荐用于开发环境）**

**Ubuntu/Debian:**
```bash
sudo apt-get install -y libdrmaa1.0 libdrmaa-dev
```

**CentOS/RHEL:**
```bash
# 需要从源码编译或使用第三方仓库
```

**方法 B: 使用 Grid Engine 自带的 DRMAA 库（推荐用于生产环境）**

如果系统已安装 Grid Engine/SGE，可以使用其自带的 DRMAA 库。通常库文件位于：
- 库文件: `/opt/gridengine/lib/lx-amd64/libdrmaa.so.1.0` 或 `/opt/gridengine/lib/lx-amd64/libdrmaa.so`
- 头文件: `/opt/gridengine/include/drmaa.h` 或 `/opt/gridengine/default/common/settings.sh` 中定义的位置

**注意**: DRMAA 库通常随 Grid Engine/SGE 系统一起安装。如果系统已安装 SGE，DRMAA 库应该已经可用。

#### 3. Go 编译器

确保已安装 Go 1.22.2 或更高版本：
```bash
go version
```

### 安装步骤

#### 方法 1: 从 GitHub 安装（推荐）

```bash
# 安装最新版本
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@latest

# 安装后，可执行文件会在 $GOPATH/bin 或 $HOME/go/bin 目录
# 确保该目录在 PATH 中：
export PATH=$PATH:$(go env GOPATH)/bin
```

#### 方法 2: 从本地源码安装

```bash
# 克隆仓库
git clone https://github.com/seqyuan/annotask.git
cd annotask

# 安装
CGO_ENABLED=1 go install ./cmd/annotask
```

#### 方法 3: 指定版本安装

```bash
# 安装特定版本（如果有 tag）
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.5.0
```

#### 方法 4: 使用 Grid Engine 自带的 DRMAA 库安装

如果使用 Grid Engine 自带的 DRMAA 库，需要设置 CGO 编译标志来指定头文件和库文件路径：

```bash
# 首先，source Grid Engine 的环境设置（如果存在）
source /opt/gridengine/default/common/settings.sh 2>/dev/null || true

# 设置 DRMAA 库路径（根据实际安装路径调整）
export DRMAA_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64/libdrmaa.so.1.0

# 查找 drmaa.h 头文件位置
DRMAA_INCLUDE=$(find /opt/gridengine -name "drmaa.h" 2>/dev/null | head -1 | xargs dirname)
DRMAA_LIB=$(find /opt/gridengine -name "libdrmaa.so*" 2>/dev/null | head -1 | xargs dirname)

# 如果找到了头文件和库文件，设置 CGO 标志
if [ -n "$DRMAA_INCLUDE" ] && [ -n "$DRMAA_LIB" ]; then
  export CGO_CFLAGS="-I$DRMAA_INCLUDE"
  export CGO_LDFLAGS="-L$DRMAA_LIB -ldrmaa"
  export LD_LIBRARY_PATH=$DRMAA_LIB:$LD_LIBRARY_PATH
fi

# 安装
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.6.0
```

**简化版本（如果已知路径）**：

```bash
# 设置 Grid Engine DRMAA 路径
export CGO_CFLAGS="-I/opt/gridengine/include"
export CGO_LDFLAGS="-L/opt/gridengine/lib/lx-amd64 -ldrmaa"
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH

# 安装
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.6.0
```

### 验证安装

```bash
# 检查可执行文件
which annotask

# 查看帮助
annotask -h
```

### 常见问题

#### 1. 编译错误: "drmaa.h: No such file or directory"

**原因**: CGO 编译器找不到 DRMAA 头文件

**解决方案**:

**方案 A: 使用 Grid Engine 自带的 DRMAA 库（推荐）**

如果系统已安装 Grid Engine，使用其自带的 DRMAA 库：

```bash
# 1. 查找 drmaa.h 头文件位置
find /opt/gridengine -name "drmaa.h" 2>/dev/null

# 2. 查找 libdrmaa.so 库文件位置
find /opt/gridengine -name "libdrmaa.so*" 2>/dev/null

# 3. 设置 CGO 编译标志（假设头文件在 /opt/gridengine/include，库文件在 /opt/gridengine/lib/lx-amd64）
export CGO_CFLAGS="-I/opt/gridengine/include"
export CGO_LDFLAGS="-L/opt/gridengine/lib/lx-amd64 -ldrmaa"
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH

# 4. 重新安装
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.6.0
```

**方案 B: 通过包管理器安装 DRMAA 开发库**

```bash
# Ubuntu/Debian
sudo apt-get install -y libdrmaa-dev

# 然后重新安装
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.6.0
```

**方案 C: 如果不需要 qsubsge 模式**

如果不需要 qsubsge 模式，可以注释掉 drmaa 相关代码（不推荐，会失去 qsubsge 功能）

#### 2. 编译错误: "sqlite3.h: No such file or directory"

**原因**: 系统未安装 SQLite3 开发库

**解决方案**: 按照上述步骤安装 libsqlite3-dev 或 sqlite-devel

#### 3. 运行时错误: "cannot find drmaa library"

**原因**: 运行时找不到 DRMAA 动态库

**解决方案**:

**如果使用 Grid Engine 自带的 DRMAA 库**:

```bash
# 查找 DRMAA 库位置
find /opt/gridengine -name "libdrmaa.so*" 2>/dev/null

# 设置库路径（例如在 /opt/gridengine/lib/lx-amd64）
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH

# 或者设置 DRMAA_LIBRARY_PATH（某些实现需要）
export DRMAA_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64/libdrmaa.so.1.0
```

**如果通过包管理器安装**:

```bash
# 查找 DRMAA 库位置
find /usr -name "libdrmaa.so*" 2>/dev/null

# 如果找到，设置库路径（例如在 /usr/lib）
export LD_LIBRARY_PATH=/usr/lib:$LD_LIBRARY_PATH
```

**永久设置（推荐）**:

将以下内容添加到 `~/.bashrc` 或 `~/.profile`:

```bash
# Grid Engine DRMAA 库路径
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH
export DRMAA_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64/libdrmaa.so.1.0
```

#### 4. CGO_ENABLED 环境变量

如果遇到 CGO 相关问题，确保设置：
```bash
export CGO_ENABLED=1
```

### 构建静态二进制文件（可选）

如果需要构建静态链接的二进制文件（不依赖系统库）：

```bash
# 注意：这需要静态链接所有依赖，可能比较复杂
CGO_ENABLED=1 go build -tags "static sqlite_omit_load_extension" -ldflags="-extldflags=-static" ./cmd/annotask
```

### 使用 Docker 安装（推荐用于生产环境）

如果目标系统环境复杂，建议使用 Docker：

```bash
# 构建 Docker 镜像
docker build -t annotask:latest .

# 运行
docker run --rm -v /path/to/data:/data annotask:latest -i /data/input.sh
```

### 卸载

```bash
# 删除可执行文件
rm $(go env GOPATH)/bin/annotask
```

