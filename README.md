<p align="center">
  <pre>
         █████╗ ███╗   ██╗███╗   ██╗ ██████╗ ████████╗ █████╗ ███████╗██╗  ██╗
        ██╔══██╗████╗  ██║████╗  ██║██╔═══██╗╚══██╔══╝██╔══██╗██╔════╝██║ ██╔╝
        ███████║██╔██╗ ██║██╔██╗ ██║██║   ██║   ██║   ███████║███████╗█████╔╝ 
        ██╔══██║██║╚██╗██║██║╚██╗██║██║   ██║   ██║   ██╔══██║╚════██║██╔═██╗ 
        ██║  ██║██║ ╚████║██║ ╚████║╚██████╔╝   ██║   ██║  ██║███████║██║  ██╗
        ╚═╝  ╚═╝╚═╝  ╚═══╝╚═╝  ╚═══╝ ╚═════╝    ╚═╝   ╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝
        
               ╭─────────────╮
               │   🐹 Go!    │
            ╭──┤  ⚡ ⚡ ⚡   ├──╮
            │  │  ░░░░░░░░  │  │  Parallel Task Execution
            │  └─────────────┘  │
            │    ╰───╯ ╰───╯    │  Local & SGE Cluster
            ╰───────────────────╯
  </pre>
</p>

<p align="center">
  <h2>annotask</h2>
  <p><strong>Annotation Task</strong> - 并行任务执行工具</p>
  <p>A Go binary for parallel task execution with local and SGE cluster support</p>
</p>

---

# 程序功能
> 程序适用于有很多运行时间短，但是需要运行很多的脚本，有助于减少投递的脚本。
> 例如有1000个cat 命令需要执行，这些命令间没有依赖关系，每个cat命令运行在2min左右

1. 支持本地并行执行和 SGE 集群投递两种模式
2. 并行的线程数可指定
3. 如果并行执行的其中某些子进程错误退出，再次执行此程序的命令可跳过成功完成的项只执行失败的子进程
4. 所有并行执行的子进程相互独立，互不影响
5. 如果并行执行的任意一个子进程退出码非0，最终annotask 也是非0退出
6. annotask会统计成功运行子脚本数量以及运行失败子脚本数量输出到stdout，如果有运行失败的脚本会输出到annotask的stderr
7. 支持自动重试机制，失败任务最多重试3次（可配置）
8. 支持内存自适应重试：如果任务因内存不足被kill，下次重试时自动增加125%的内存
9. 实时监控任务状态，输出到标准输出
10. 支持项目管理和任务状态查询

# 安装

## 前置条件

1. SQLite3 开发库
2. DRMAA 库（如果使用 qsubsge 模式）
3. Go 编译器 1.22.2 或更高版本

## 编译时设置环境变量

编译程序时需要设置 CGO 环境变量，指向 Grid Engine 的 DRMAA 库。有两种编译方式：

### 方法 1：使用 rpath（推荐，无需运行时环境变量）

使用 `-Wl,-rpath` 选项将库路径嵌入到二进制文件中，运行时无需设置 `LD_LIBRARY_PATH`：

```bash
# 设置 Grid Engine DRMAA 路径，并使用 rpath 嵌入库路径
export CGO_CFLAGS="-I/opt/gridengine/include"
export CGO_LDFLAGS="-L/opt/gridengine/lib/lx-amd64 -ldrmaa -Wl,-rpath,/opt/gridengine/lib/lx-amd64"
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH

# 安装（从 GitHub 下载并编译指定版本）
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@latest
```

**优点**：编译后的二进制文件可直接运行，不需要运行时设置环境变量或包装脚本。

### 方法 2：使用运行时包装脚本

如果不使用 rpath，需要在运行时设置 `LD_LIBRARY_PATH`。创建一个包装脚本（例如 `/home/seqyuan/go/bin/annotask1`）：

```bash
# 编译时
export CGO_CFLAGS="-I/opt/gridengine/include"
export CGO_LDFLAGS="-L/opt/gridengine/lib/lx-amd64 -ldrmaa"
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.7.5
```

```bash
# 运行时包装脚本（例如 /home/seqyuan/go/bin/annotask）
#!/bin/bash
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH
/home/seqyuan/go/bin/annotask_linux "$@"
```

**注意**：
- `CGO_CFLAGS` 和 `CGO_LDFLAGS` 只在编译时需要
- `LD_LIBRARY_PATH` 在编译时用于链接器找到库，如果使用方法 1 的 rpath，运行时不需要
- 如果使用方法 2，运行时需要包装脚本设置 `LD_LIBRARY_PATH`

如果使用方法 2，将包装脚本设置为可执行：
```bash
chmod +x /home/seqyuan/go/bin/annotask
```

## 如何查找环境变量

如果不知道 Grid Engine 的安装路径，可以使用以下命令查找：

```bash
# 查找 drmaa.h 头文件位置
find /opt/gridengine -name "drmaa.h" 2>/dev/null

# 查找 libdrmaa.so 库文件位置
find /opt/gridengine -name "libdrmaa.so*" 2>/dev/null
```

找到路径后：
- 将头文件所在目录设置为 `CGO_CFLAGS`（例如：`-I/opt/gridengine/include`）
- 将库文件所在目录设置为 `CGO_LDFLAGS`（例如：`-L/opt/gridengine/lib/lx-amd64 -ldrmaa`）
- 如果使用方法 1（rpath），在 `CGO_LDFLAGS` 中添加 `-Wl,-rpath,/opt/gridengine/lib/lx-amd64`
- 编译时也需要设置 `LD_LIBRARY_PATH` 以便链接器找到库

## 配置文件

首次运行 `annotask` 时，会在程序所在目录自动创建 `annotask.yaml` 配置文件。配置文件包含：

- `db`: 全局数据库路径（记录所有任务）
- `project`: 默认项目名称
- `retry.max`: 最大重试次数
- `queue`: SGE 默认队列
- `node`: SGE 节点名称（qsubsge 模式会检查）
- `defaults`: 各参数的默认值

配置文件示例见 `annotask.yaml.example`。

### 全局数据库权限设置

如果多个用户或多进程需要访问全局数据库，需要设置相应的文件权限。假设全局数据库路径为 `/path/to/annotask.db`：

```bash
# 对上一级文件夹设置权限
chmod 777 $(dirname /path/to/annotask.db)

# 对数据库文件设置权限
chmod 777 /path/to/annotask.db
```

这样确保所有用户和进程都可以读取和写入全局数据库。

# 使用方法

## 运行模式

annotask 支持两种运行模式：

1. **Local 模式**（默认）：在本地并行执行任务
2. **QsubSge 模式**：将任务投递到 SGE 集群执行

## Local 模式

### 基本用法

```bash
annotask -i input.sh -l 2 -p 4 --project myproject
```

### 参数说明

```
-i, --infile    输入文件，shell脚本格式（必需）
-l, --line      每几行作为一个任务单元（默认：1）
-p, --thread    最大并发任务数（默认：1）
    --project   项目名称（默认：从配置文件读取）
```

### 使用示例

`annotask -i input.sh -l 2 -p 2 --project test`

标准错物流的输出：

```
[1 2 3 4 5]
All works: 5
Successed: 3
Error: 2
Err Shells:
2	/Volumes/RD/parrell_task/input.sh.shell/work_0002.sh
3	/Volumes/RD/parrell_task/input.sh.shell/work_0003.sh
```

运行产生的目录结构：
```
.
├── input.sh
├── input.sh.db
└── input.sh.shell
    ├── work_0001.sh
    ├── work_0001.sh.e
    ├── work_0001.sh.o
    ├── work_0001.sh.sign
    ├── work_0002.sh
    ├── work_0002.sh.e
    ├── work_0002.sh.o
    ├── work_0003.sh
    ├── work_0003.sh.e
    ├── work_0003.sh.o
    ├── work_0004.sh
    ├── work_0004.sh.e
    ├── work_0004.sh.o
    ├── work_0004.sh.sign
    ├── work_0005.sh
    ├── work_0005.sh.e
    ├── work_0005.sh.o
    └── work_0005.sh.sign
```

## QsubSge 模式

### 基本用法

```bash
# 不指定 h_vmem，将自动使用 mem * 1.25
annotask qsubsge -i input.sh -l 2 -p 4 --project myproject --cpu 2 --mem 4

# 或者手动指定 h_vmem
annotask qsubsge -i input.sh -l 2 -p 4 --project myproject --cpu 2 --mem 4 --h_vmem 8
```

### 参数说明

```
-i, --infile    输入文件，shell脚本格式（必需）
-l, --line      每几行作为一个任务单元（默认：从配置文件读取）
-p, --thread    最大并发任务数（默认：从配置文件读取）
    --project   项目名称（默认：从配置文件读取）
    --cpu       CPU数量（默认：从配置文件读取）
    --mem       内存大小（GB，默认：从配置文件读取）
    --h_vmem    虚拟内存大小（GB，可选，不设置时默认为 mem * 1.25）
```

### 注意事项

- QsubSge 模式会检查当前节点是否与配置文件中的 `node` 一致
- 如果不一致，程序会报错退出
- 任务会自动投递到 SGE 集群，输出文件会生成在子脚本所在目录（`{输入文件路径}.shell`）
- 输出文件格式为 `{文件前缀}_0001.o.{jobID}` 和 `{文件前缀}_0001.e.{jobID}`
- 例如：输入文件为 `input.sh`，子任务为 `input_0001.sh`，则输出文件为：
  - `input.sh.shell/input_0001.o.{jobID}`（标准输出）
  - `input.sh.shell/input_0001.e.{jobID}`（标准错误）

## 输入文件格式

`-i` 参数为一个shell脚本，例如`input.sh`这个shell脚本的内容示例如下：

```
echo 1
echo 11
echo 2
sddf
echo 3
grep -h
echo 4
echo 44
echo 5
echo 6
```

### -l 参数说明

依照上面的示例，一共有10行命令，如果设置 `-l 2`，则每2行作为1个单位并行的执行。

### -p 参数说明

如果要对整个annotask程序所在进程的资源做限制，可设置`-p`参数，指定最多同时并行多少个子进程。

### annotask产生的文件

1. `input.sh.db`文件，此文件为sqlite数据库（本地任务数据库）
2. `input.sh.shell`目录，子脚本存放目录
3. 按照`-l`参数切割的input.sh的子脚本，存放在`input.sh.shell`目录
4. 子脚本命名格式：`{文件前缀}_0001.sh`（例如 `input.sh` 会生成 `input_0001.sh`，最多支持9999个子任务）
5. 每个子脚本的标准输出和标准错误会分别保存到 `.o` 和 `.e` 文件

## 任务状态查询

### 查询所有任务

```bash
annotask stat
```

示例输出：
```
Tasks for user: username
----------------------------------------------------------------------------------------------------------------------
Project         Module                Mode       Pending    Failed     Running    Finished   Start Time  End Time
----------------------------------------------------------------------------------------------------------------------
myproject       input                 local      0          0          0          5          12-25 14:30  12-25 15:45
myproject       process               qsubsge    2          1          3          10         12-26 09:15  -
testproject     analysis              local      0          0          0          8          12-24 10:20  12-24 11:30
----------------------------------------------------------------------------------------------------------------------
Total records: 3
```

### 查询特定项目

```bash
annotask stat -p myproject
```

示例输出：
```
module               pending  running  failed   finished  stime        etime       
input                0        0        0        5         12-25 14:30  12-25 15:45
process              2        3        1        10        12-26 09:15  -

input_/absolute/path/to/input.sh
process_/absolute/path/to/process.sh
```

注意：使用 `-p` 参数时会自动显示表格和 shellPath 列表。

## 删除任务记录

### 删除整个项目

```bash
annotask delete -p myproject
```

### 删除特定模块

```bash
annotask delete -p myproject -m input
```

## 其他使用方式

```bash
annotask -i input.sh -l 2 -p 2 --project test
```

我们可以把以上命令写入到`work.sh`里，然后把`work.sh`投递到SGE或者K8s计算节点

# 数据库结构

## 本地任务数据库（input.sh.db）

annotask会针对每一个输入脚本，在脚本所在目录生成`脚本名称`+`.db`的sqlite3数据库，用于记录各`子脚本`的运行状态，例如`input.sh`对应的数据库名称为`input.sh.db`

`input.sh.db`这个sqlite3数据库有1个名为`job`的table，`job`主要包含以下几列：

```
Id          INTEGER PRIMARY KEY AUTOINCREMENT  # 自增ID
subJob_num  INTEGER UNIQUE NOT NULL            # 子任务编号
shellPath   TEXT                               # 子脚本路径
status      TEXT                               # 任务状态
exitCode    INTEGER                            # 退出码
retry       INTEGER DEFAULT 0                 # 重试次数
starttime   DATETIME                           # 开始时间
endtime     DATETIME                           # 结束时间
mode        TEXT DEFAULT 'local'               # 运行模式（local/qsubsge）
cpu         INTEGER DEFAULT 1                 # CPU数量（qsubsge模式）
mem         INTEGER DEFAULT 1                 # 内存大小（GB，qsubsge模式）
h_vmem      INTEGER DEFAULT 1                 # 虚拟内存大小（GB，qsubsge模式，未设置时默认为mem*1.25）
taskid      TEXT                               # 任务ID（local模式为PID，qsubsge模式为Job ID）
```

*  **subJob_num** 列表示记录的是第几个子脚本
*  **shellPath**为对应子脚本路径
*  **status**表示对应子脚本的状态，状态有4种: Pending Failed Running Finished
*  **exitCode**为对应子脚本的退出码
*  **retry**为如果子脚本出错的情况下annotask程序自动重新尝试运行该出错子进程的次数（最多3次）
*  **starttime**为子脚本开始运行的时间
*  **endtime**为子脚本结束运行的时间
*  **mode**为运行模式：`local` 或 `qsubsge`
*  **taskid**为任务ID：local模式存储进程PID，qsubsge模式存储SGE Job ID

## 全局任务数据库（annotask.db）

annotask会在程序所在目录创建全局数据库`annotask.db`（路径可在配置文件中修改），用于记录所有任务的总体状态。

`annotask.db`包含一个`tasks`表，主要字段：

```
Id              INTEGER PRIMARY KEY AUTOINCREMENT
usrID           TEXT NOT NULL                    # 用户ID
project         TEXT NOT NULL                    # 项目名称
module          TEXT NOT NULL                    # 模块名称（输入文件basename）
mode            TEXT NOT NULL                    # 运行模式
starttime       DATETIME NOT NULL                # 启动时间
endtime         DATETIME                         # 结束时间
shellPath       TEXT NOT NULL                    # 输入文件完整路径
totalTasks      INTEGER DEFAULT 0                # 子任务总数
pendingTasks    INTEGER DEFAULT 0                # Pending状态任务数
failedTasks     INTEGER DEFAULT 0                # Failed状态任务数
runningTasks    INTEGER DEFAULT 0                # Running状态任务数
finishedTasks   INTEGER DEFAULT 0               # Finished状态任务数
```

## 实时监控

annotask在运行时会启动一个独立的goroutine实时监控任务状态，并将状态变化输出到标准输出，包括：

- 新任务启动（标记为 `[NEW]`）
- 任务状态变化
- 任务重试（标记为 `[RETRY-N]`，N为重试次数）
- 任务完成或失败

# 功能特性

## 自动重试机制

- 失败的任务会自动重试，最多重试3次（可在配置文件中修改）
- 如果任务因内存不足被SGE系统kill，下次重试时会自动将内存请求增加125%
- 重试次数记录在数据库的`retry`列中

## 内存自适应

在qsubsge模式下，如果任务因为内存不足被kill（退出码137或错误日志中包含内存相关关键词），annotask会自动：

1. 检测内存错误
2. 将`mem`和`h_vmem`增加125%
3. 重新投递任务

## 任务状态监控

annotask在运行时会实时输出任务状态，格式如下：

```
[NEW] Task 1: Pending -> Running (PID: 12345)
Task 2: Running -> Finished (Exit: 0)
[RETRY-1] Task 3: Failed -> Running (PID: 12346)
```

# 常见问题

## 并行子进程中其中有些子进程出错怎么办？

例如示例所示`input.sh`中的第2个和第3个子脚本出错，那么待`input.sh`退出后，修正子脚本的命令行，再重新运行或者投递`input.sh`即可。在重新运行`work.sh`时，annotask会自动跳过已经成功完成的子脚本，只运行出错的子脚本。

如果任务失败，annotask会自动重试（最多3次），无需手动重新运行。

## 编译错误: "drmaa.h: No such file or directory"

**原因**: 系统未安装 DRMAA 开发库

**解决方案**:
- 如果不需要 qsubsge 模式，可以忽略此错误（不影响local模式）
- 如果需要 qsubsge 模式，必须安装 DRMAA 库（通常随SGE系统一起安装）

## 编译错误: "sqlite3.h: No such file or directory"

**原因**: 系统未安装 SQLite3 开发库

**解决方案**: 按照安装说明安装 libsqlite3-dev 或 sqlite-devel

## QsubSge 模式报错: "current node does not match config node"

**原因**: 当前节点与配置文件中的`node`设置不一致

**解决方案**: 
- 修改配置文件中的`node`为当前节点名称
- 或者将`node`设置为空字符串（会自动使用当前主机名）

