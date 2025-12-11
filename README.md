<p align="center">
  <img src="https://raw.githubusercontent.com/seqyuan/annotask-doc/main/.vuepress/public/logo.svg" alt="annotask logo" width="200" height="200">
</p>

<p align="center">
  <h2>annotask</h2>
  <p><strong>Annotation Task</strong> - 并行任务执行工具</p>
  <p>A Go binary for parallel task execution with local and SGE cluster support</p>
</p>

---

# 程序功能

`annotask` 是一个高效的并行任务执行工具，专为处理大量独立、需要并行运行的脚本任务而设计。通过智能的任务分组、并发控制和状态管理，显著减少任务投递开销，提高执行效率。

## 核心特性

### 🚀 双模式执行
- **本地并行模式（local）**：在本地机器上并行执行任务，适合单机多核环境
- **SGE 集群模式（qsubsge）**：通过 DRMAA 接口将任务投递到 SGE 集群执行，支持大规模分布式计算
  - 支持两种并行环境模式：默认模式（`-l p=X`）和 PE SMP 模式（`-pe smp X`）
  - 灵活的资源管理：可指定 CPU、内存（vf/h_vmem）、队列、SGE 项目等参数
  - 节点安全检查：防止在计算节点误投递任务

### 📦 智能任务管理
- **任务分组**：支持将输入文件按行分组（`-l` 参数），将多个命令合并为一个任务单元执行
- **断点续传**：基于 SQLite 数据库记录任务状态，支持中断后继续执行
  - 已成功完成的任务会被自动跳过，只执行失败或未执行的任务
  - 每个任务独立执行，互不影响，失败任务不会阻塞其他任务
- **并发控制**：可指定最大并发任务数（`-p` 参数），灵活控制资源使用

### 🔄 自动重试机制
- **智能重试**：失败任务最多自动重试 3 次（可在配置文件中自定义）
- **内存自适应重试**：在 qsubsge 模式下，如果任务因内存不足被 SGE kill
  - 自动检测内存相关错误（OOM、h_vmem 超限等）
  - 下次重试时自动将用户显式设置的内存参数增加 125%（向上取整）
  - 仅针对用户显式设置的内存参数（`--mem` 或 `--h_vmem`）进行自适应调整

### 📊 实时监控与状态跟踪
- **实时状态输出**：以表格形式实时输出任务状态变化到标准输出
  - 显示字段：重试轮次、任务编号、状态、任务ID、退出码、时间
  - 每秒更新一次，及时反馈任务执行情况
- **全局任务数据库**：记录所有项目的任务执行历史
  - 支持按项目查询任务状态（`stat` 模块）
  - 支持删除任务记录（`delete` 模块）
  - 便于任务管理和历史追溯

### 📝 完善的日志与输出
- **独立日志文件**：每个任务的 stdout 和 stderr 输出到独立文件（`.o` 和 `.e` 文件）
- **执行统计**：程序结束时统计成功和失败的任务数量
  - 成功和失败数量输出到标准输出
  - 失败任务的路径输出到标准错误
  - 如果有任何任务失败，程序退出码为非 0

### 🎯 项目管理
- **项目组织**：支持通过项目名称组织和管理任务
- **两级配置管理**：支持用户级和系统级配置文件
  - 用户配置文件（`~/.annotask/annotask.yaml`）：用户个人默认设置，优先级高
  - 系统配置文件（程序目录下的 `annotask.yaml`）：系统级默认设置
  - 首次空运行 `annotask` 时自动创建用户配置文件
  - 配置优先级：命令行参数 > 用户配置 > 系统配置 > 程序默认值

## 适用场景

- **批量数据处理**：大量独立的任务，如批量文件处理、数据转换等
- **生物信息学分析**：大量样本的独立分析任务，如序列比对、注释等
- **并行计算任务**：需要并发执行但相互独立的计算任务
- **集群作业管理**：需要将大量小任务投递到 SGE 集群的场景

# 安装
### 安装命令
```bash
# 设置 Grid Engine DRMAA 路径，并使用 rpath 嵌入库路径
export CGO_CFLAGS="-I/opt/gridengine/include"
export CGO_LDFLAGS="-L/opt/gridengine/lib/lx-amd64 -ldrmaa -Wl,-rpath,/opt/gridengine/lib/lx-amd64"
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH

# 安装（从 GitHub 下载并编译指定版本）
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.8.7
```

```bash
which annotask
```
## 配置文件

`annotask` 支持两级配置文件系统，提供灵活的配置管理：

### 配置文件位置和优先级

1. **用户配置文件**：`~/.annotask/annotask.yaml`（用户 home 目录下的 `.annotask` 目录）
   - 优先级：高（仅次于命令行参数）
   - 首次空运行 `annotask` 时自动创建
   - 适用于个人默认设置（如默认队列、重试次数等）

2. **系统配置文件**：程序所在目录的 `annotask.yaml`
   - 优先级：中（低于用户配置）
   - 首次运行 `annotask` 时自动创建
   - 适用于系统级默认设置

3. **配置优先级**（从高到低）：
   - 命令行参数（`--queue`, `-P/--sge-project` 等）
   - 用户配置文件（`~/.annotask/annotask.yaml`）
   - 系统配置文件（`annotask.yaml`）
   - 程序默认值

### 用户配置文件（~/.annotask/annotask.yaml）

首次空运行 `annotask` 时，会在用户 home 目录下的 `.annotask` 目录自动创建 `annotask.yaml` 文件，默认内容：

```yaml
db: ~/.annotask/annotask.db
retry:
  max: 3
queue: sci.q
sge_project: ""
```

**配置说明**：
- `db`: 全局数据库路径，默认为 `~/.annotask/annotask.db`
  - 如果配置的 db 路径不存在，程序会自动回退到系统配置的 db 路径
  - 如果系统配置的 db 路径也不存在，则使用用户配置的 db 路径（会自动创建）
- `retry.max`: 最大重试次数，默认为 3
- `queue`: SGE 默认队列，默认为 `sci.q`
- `sge_project`: SGE 项目名称，默认为空

**使用场景**：
- 设置个人默认队列（如 `queue: sci.q`）
- 设置个人默认重试次数（如 `retry.max: 5`）
- 设置个人数据库路径（如 `db: /path/to/custom/annotask.db`）

**示例**：
```bash
# 空运行 annotask，自动创建用户配置文件
annotask

# 编辑用户配置文件
vim ~/.annotask/annotask.yaml

# 之后运行 qsubsge 时，如果没有指定 --queue，会自动使用用户配置中的 queue
annotask qsubsge -i input.sh  # 使用 ~/.annotask/annotask.yaml 中的 queue: sci.q
annotask qsubsge -i input.sh --queue trans.q  # 命令行参数优先，使用 trans.q
```

### 系统配置文件（annotask.yaml）

系统配置文件包含完整的配置选项：

- `db`: 全局数据库路径（记录所有任务）
- `project`: 默认项目名称
- `retry.max`: 最大重试次数
- `queue`: SGE 默认队列
- `sge_project`: SGE 项目名称（用于资源配额管理）
- `node`: 允许使用 qsubsge 模式的节点列表（列表格式，支持多个节点）
  - 如果为空或不设置，则不对 qsubsge 模式做节点限制
  - 如果设置了节点列表，当前节点必须在列表中才能使用 qsubsge 模式
- `defaults`: 各参数的默认值
  - `line`: 默认行分组数
  - `thread`: 默认并发线程数
  - `cpu`: 默认 CPU 数量
- `monitor_update_interval`: 全局数据库更新间隔（秒，默认 60）

配置文件示例见 `annotask.yaml.example`。

## 全局数据库权限设置

如果多个用户或多进程需要访问全局数据库（配置文件中 `db` 字段指定的路径），需要设置相应的文件权限：

```bash
# 假设全局数据库路径为 /path/to/annotask.db
# 对上一级文件夹设置权限
chmod 777 $(dirname /path/to/annotask.db)

# 对数据库文件设置权限
chmod 777 /path/to/annotask.db
```

这样确保所有用户和进程都可以读取和写入全局数据库。


## Tips
#### 如何查找环境变量

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


## 卸载

```bash
# 删除可执行文件
rm $(go env GOPATH)/bin/annotask
```


# 使用方法

## 运行模式

annotask 支持两种运行模式：

1. **local 模式**（默认）：在本地并行执行任务
2. **qsubsge 模式**：将任务投递到 SGE 集群执行

## local 模式

### 基本用法

```bash
annotask -i input.sh -l 2 -p 4 --project myproject
```

### 参数说明

```
-i, --infile    输入文件，shell脚本（必需）
-l, --line      每几行作为一个任务单元（默认：1）
-p, --thread    最大并发任务数（默认：1）
    --project   项目名称（默认：从用户配置或系统配置读取）
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
2	/Volumes/RD/parrell_task/input.sh.shell/task_0002.sh
3	/Volumes/RD/parrell_task/input.sh.shell/task_0003.sh
```

运行产生的目录结构：
```
.
├── input.sh
├── input.sh.db
└── input.sh.shell
    ├── task_0001.sh
    ├── task_0001.sh.e
    ├── task_0001.sh.o
    ├── task_0001.sh.sign
    ├── task_0002.sh
    ├── task_0002.sh.e
    ├── task_0002.sh.o
    ├── task_0003.sh
    ├── task_0003.sh.e
    ├── task_0003.sh.o
    ├── task_0004.sh
    ├── task_0004.sh.e
    ├── task_0004.sh.o
    ├── task_0004.sh.sign
    ├── task_0005.sh
    ├── task_0005.sh.e
    ├── task_0005.sh.o
    └── task_0005.sh.sign
```

## qsubsge 模式

### 基本用法

```bash
# 只设置 mem，DRMAA 投递时只使用 -l vf=XG（虚拟内存）
annotask qsubsge -i input.sh -l 2 -p 4 --project myproject --cpu 2 --mem 4

# 只设置 h_vmem，DRMAA 投递时只使用 -l h_vmem=XG（硬虚拟内存限制）
annotask qsubsge -i input.sh -l 2 -p 4 --project myproject --cpu 2 --h_vmem 8

# 同时设置 mem 和 h_vmem
annotask qsubsge -i input.sh -l 2 -p 4 --project myproject --cpu 2 --mem 4 --h_vmem 8

# 指定队列（单个或多个，逗号分隔）
annotask qsubsge -i input.sh --queue sci.q
annotask qsubsge -i input.sh --queue trans.q,nassci.q,sci.q

# 指定 SGE 项目（用于资源配额管理）
annotask qsubsge -i input.sh -P bioinformatics

# 使用 -pe smp 并行环境模式（默认）
annotask qsubsge -i input.sh --cpu 4 --h_vmem 5
# 或显式指定
annotask qsubsge -i input.sh --cpu 4 --h_vmem 5 --mode pe_smp

# 使用 -l p=X 模式
annotask qsubsge -i input.sh --cpu 4 --h_vmem 18 --mode num_proc
```

### 参数说明

```
-i, --infile    输入文件，shell脚本格式（必需）
-l, --line      每几行作为一个任务单元（默认：从用户配置或系统配置读取）
-p, --thread    最大并发任务数（默认：从用户配置或系统配置读取）
    --project   项目名称（默认：从用户配置或系统配置读取）
    --cpu       CPU数量（默认：从用户配置或系统配置读取）
    --mem       虚拟内存（vf）大小（GB，映射到 -l vf=XG，仅在显式设置时在DRMAA中使用）
    --h_vmem    硬虚拟内存限制（h_vmem）大小（GB，映射到 -l h_vmem=XG，仅在显式设置时在DRMAA中使用）
    --queue     队列名称（多个队列用逗号分隔，默认：从用户配置或系统配置读取）
    -P, --sge-project  SGE项目名称（用于资源配额管理，默认：从用户配置或系统配置读取）
    --mode      并行环境模式：pe_smp（使用 -pe smp X，默认）或 num_proc（使用 -l p=X）
```

**重要说明**：
- `--mem` 和 `--h_vmem` 参数只有在用户显式设置时，才会在 DRMAA 投递时使用
- `--mem` 对应 SGE 的 `vf` 资源（虚拟内存），DRMAA 投递时使用 `-l vf=XG`
- `--h_vmem` 对应 SGE 的 `h_vmem` 资源（硬虚拟内存限制），DRMAA 投递时使用 `-l h_vmem=XG`
- 如果只设置了 `--mem`，DRMAA 投递时只包含 `-l vf=XG`，不包含 `-l h_vmem`
- 如果只设置了 `--h_vmem`，DRMAA 投递时只包含 `-l h_vmem=XG`，不包含 `-l vf`
- 如果都不设置，DRMAA 投递时不会包含内存相关参数
- `--queue` 支持多个队列，用逗号分隔（例如：`trans.q,nassci.q,sci.q`）
- `-P/--sge-project` 用于 SGE 资源配额管理，如果未设置则不在 DRMAA 中使用 `-P` 参数

**并行环境模式**：
- **pe_smp 模式**（默认，`--mode pe_smp`）：使用 `-pe smp X` 指定 CPU 数量
  - 示例：`--cpu 4 --h_vmem 5` → `-l h_vmem=5G -pe smp 4`
  - 这里的内存指的是单 CPU 需要消耗的内存
- **num_proc 模式**（`--mode num_proc`）：使用 `-l p=X` 指定 CPU 数量
  - 示例：`--cpu 4 --h_vmem 18 --mode num_proc` → `-l h_vmem=18G,p=4`
  - 这里的内存指的是总内存

### 注意事项

- qsubsge 模式会检查当前节点是否在配置文件中的 `node` 列表中，以防止在计算节点投递任务
- 如果配置文件中设置了 `node` 列表，当前节点必须在列表中才能使用 qsubsge 模式
- 如果 `node` 为空或不设置，则不对节点做限制
- 如果当前节点不在允许的列表中，程序会报错退出
- 任务会自动投递到 SGE 集群，输出文件会生成在子脚本所在目录（`{输入文件路径}.shell`）
- 输出文件格式为 `task_0001.o.{jobID}` 和 `task_0001.e.{jobID}`
- 例如：输入文件为 `input.sh`，子任务为 `task_0001.sh`，则输出文件为：
  - `input.sh.shell/task_0001.o.{jobID}`（标准输出）
  - `input.sh.shell/task_0001.e.{jobID}`（标准错误）

## 输入文件格式

`-i` 参数为一个shell脚本，例如`input.sh`这个shell脚本的内容示例如下：

```
blastn -db /seqyuan/nt -evalue 0.001 -outfmt 5  -query sample1_1.fasta -out sample1_1.xml -num_threads 4
python3 /seqyuan/bin/blast_xml2txt.py -i sample1_1.xml -o sample1_1.txt
blastn -db /seqyuan/nt -evalue 0.001 -outfmt 5  -query sample2_1.fasta -out sample2_1.xml -num_threads 4
python3 /seqyuan/bin/blast_xml2txt.py -i sample2_1.xml -o sample2_1.txt
blastn -db /seqyuan/nt -evalue 0.001 -outfmt 5  -query sample3_1.fasta -out sample3_1.xml -num_threads 4
python3 /seqyuan/bin/blast_xml2txt.py -i sample3_1.xml -o sample3_1.txt
blastn -db /seqyuan/nt -evalue 0.001 -outfmt 5  -query sample4_1.fasta -out sample4_1.xml -num_threads 4
python3 /seqyuan/bin/blast_xml2txt.py -i sample4_1.xml -o sample4_1.txt
```

### -l 参数说明

依照上面的示例，一共有8行命令，如果设置 `-l 2`，则每2行作为1个单位并行的执行。

### -p 参数说明

如果要对整个annotask程序所在进程的资源做限制，可设置`-p`参数，指定最多同时并行多少个子进程。

### annotask产生的文件

1. `input.sh.db`文件，此文件为sqlite数据库（本地任务数据库）
2. `input.sh.shell`目录，子脚本存放目录
3. 按照`-l`参数切割的input.sh的子脚本，存放在`input.sh.shell`目录
4. 子脚本命名格式：`task_0001.sh`（固定使用 `task` 作为前缀，最多支持9999个子任务）
5. 每个子脚本的标准输出和标准错误会分别保存到 `.o` 和 `.e` 文件

## 任务状态查询

### 查询所有任务

```bash
annotask stat
```

示例输出：
```
project          module               mode       status     statis          stime        etime       
myproject        input                local      -          5/0             12-25 14:30  12-25 15:45
myproject        process              qsubsge    -          16/2            12-26 09:15  -
testproject      analysis             local      -          8/0             12-24 10:20  12-24 11:30
```

**输出说明**：
- `project`: 项目名称
- `module`: 模块名称（基于输入文件名）
- `mode`: 执行模式（local 或 qsubsge）
- `status`: 任务状态（可能为 "-"）
- `statis`: 任务统计，格式为 `总任务数/待处理任务数`（例如：`16/2` 表示总共16个任务，2个待处理）
- `stime`: 开始时间（格式：MM-DD HH:MM）
- `etime`: 结束时间（格式：MM-DD HH:MM，未结束显示 "-"）

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

**输出说明**：
- 第一部分：任务状态表格
  - `module`: 模块名称
  - `pending`: 待处理任务数
  - `running`: 运行中任务数
  - `failed`: 失败任务数
  - `finished`: 已完成任务数
  - `stime`: 开始时间（格式：MM-DD HH:MM）
  - `etime`: 结束时间（格式：MM-DD HH:MM，未结束显示 "-"）
- 第二部分：shell 路径列表（空行分隔）
  - 格式：`模块名_完整shell路径`
  - 每个模块对应一行，用于快速定位任务文件

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
mem         INTEGER DEFAULT 1                 # 虚拟内存（vf）大小（GB，qsubsge模式，映射到 -l vf=XG，仅在用户显式设置时使用）
h_vmem      INTEGER DEFAULT 1                 # 硬虚拟内存限制（h_vmem）大小（GB，qsubsge模式，映射到 -l h_vmem=XG，仅在用户显式设置时使用）
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

annotask在运行时会启动一个独立的goroutine实时监控任务状态，并将状态变化以表格格式输出到标准输出。

### 监控输出格式

监控输出采用表格格式，包含以下列：

```
try    task   status     taskid     exitcode time        
1:3    0001   Running    3652318    -        12-09 10:24 
1:3    0002   Running    3652321    -        12-09 10:24 
1:3    0003   Failed     3652312    1        12-09 10:25 
```

**列说明**：
- `try`: 当前重试轮次/最大重试次数（例如：`1:3` 表示第1轮，最多3次）
- `task`: 任务编号（4位数字，例如：`0001`）
- `status`: 任务状态（Running, Failed, Finished）
- `taskid`: 任务ID（local模式为PID，qsubsge模式为Job ID）
- `exitcode`: 退出码（如果任务已完成）
- `time`: 时间（MM-DD HH:MM格式）

**注意**：
- 表头只在第一次输出时显示一次
- Pending 状态的任务不会显示在监控输出中
- 时间格式为"月-日 时:分"（例如：`12-09 10:24`）

# 功能特性

## 自动重试机制

- 失败的任务会自动重试，最多重试3次（可在配置文件中修改）
- 如果任务因内存不足被SGE系统kill，下次重试时会自动将内存请求增加125%
- 重试次数记录在数据库的`retry`列中

## 内存自适应

在qsubsge模式下，如果任务因为内存不足被kill（退出码137或错误日志中包含内存相关关键词），annotask会自动：

1. 检测内存错误
2. 根据用户设置的参数，只增加相应参数的内存（向上取整）：
   - 如果用户只设置了 `--mem`，只增加 `mem`（增加125%，向上取整）
   - 如果用户只设置了 `--h_vmem`，只增加 `h_vmem`（增加125%，向上取整）
   - 如果用户同时设置了两个参数，两个都增加（增加125%，向上取整）
   - 如果用户都没有设置，不进行内存增加
3. 重新投递任务

## 任务状态监控

annotask在运行时会实时输出任务状态，采用表格格式显示。详见"实时监控"章节。

# 常见问题

## 并行子进程中其中有些子进程出错怎么办？

例如示例所示`input.sh`中的第2个和第3个子脚本出错，那么待`input.sh`退出后，修正子脚本的命令行，再重新运行或者投递`input.sh`即可。在重新运行`work.sh`时，annotask会自动跳过已经成功完成的子脚本，只运行出错的子脚本。

如果任务失败，annotask会自动重试（最多3次），无需手动重新运行。

