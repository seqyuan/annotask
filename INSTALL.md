# 安装说明

## 编译时设置环境变量

编译程序时需要设置 CGO 环境变量，指向 Grid Engine 的 DRMAA 库。使用 `-Wl,-rpath` 选项将库路径嵌入到二进制文件中，运行时无需设置 `LD_LIBRARY_PATH`：

```bash
# 设置 Grid Engine DRMAA 路径，并使用 rpath 嵌入库路径
export CGO_CFLAGS="-I/opt/gridengine/include"
export CGO_LDFLAGS="-L/opt/gridengine/lib/lx-amd64 -ldrmaa -Wl,-rpath,/opt/gridengine/lib/lx-amd64"
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH

# 
go env -w GOPROXY=https://goproxy.cn,direct
# go env -w GOPROXY=https://mirrors.tuna.tsinghua.edu.cn/goproxy/,direct

# 安装（从 GitHub 下载并编译指定版本）
# CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.9.7
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@latest
```

## 验证安装

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

### 用户配置文件（个性化配置）

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
  - **注意**：虽然用户配置文件中可以设置 `db` 路径，但程序实际使用的全局数据库路径来自系统配置文件（程序目录下的 `annotask.yaml`）
  - 如果系统配置的 db 路径不存在，程序会自动回退到用户配置的 db 路径（会自动创建）
  - 如果用户配置的 db 路径也不存在，则使用默认路径 `~/.annotask/annotask.db`（会自动创建）
- `retry.max`: 最大重试次数，默认为 3
- `queue`: SGE 默认队列，默认为 `sci.q`
- `sge_project`: SGE 项目名称，默认为空

**使用场景**：
- 设置个人默认队列（如 `queue: sci.q`）
- 设置个人默认重试次数（如 `retry.max: 5`）
- 设置个人数据库路径（如 `db: /path/to/custom/annotask.db`，但实际生效的是系统配置的 db 路径）

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

### 系统配置文件（总配置）

系统配置文件位于程序所在目录的 `annotask.yaml`，包含完整的配置选项。首次运行 `annotask` 时，如果该文件不存在，程序会自动创建默认配置文件。

系统配置文件示例（`annotask.yaml.example`）：

```yaml
# annotask configuration file
# This file should be placed in the same directory as the annotask executable

# Global database path (sqlite3)
# This database records all annotask runs
# 注意：这是实际使用的全局数据库路径，所有 annotask 实例共享此数据库
db: ./annotask.db

# Default project name
project: anno

# Retry configuration
retry:
  max: 3  # Maximum retry times for failed tasks

# Default queue for qsubsge mode
queue: sci.q

# SGE project name for qsubsge mode (optional, for resource quota management)
# If not set, jobs will use default project or no project
sge_project: ""

# SGE environment settings.sh path (optional, for qsubsge mode)
# If set, this path will be used to load SGE environment variables
# If not set, program will auto-detect settings.sh from common installation paths
# This should only be set in system config (not in user config) for unified management
# Example: /opt/gridengine/default/common/settings.sh
sgeenv: /opt/gridengine/default/common/settings.sh

# Allowed node names for qsubsge mode (list format)
# If empty or not set, no node restriction will be applied
# In qsubsge mode, program will check if current node is in this list
# If current node is in the list, qsubsge mode can be used
node:
  - bj-sci-login1
  - login-0-2

# Default parameter values
defaults:
  line: 1      # Default number of lines to group as one task
  thread: 10   # Default max concurrent tasks (note: command line default is 10, this is for reference only)
  cpu: 1       # Default CPU count (for qsubsge mode)
  # Note: mem and h_vmem are not configured here. They must be explicitly set
  # via --mem and --h_vmem flags if needed for qsubsge mode

# Monitor update interval (in seconds)
# Controls how often the global database is updated with task status
# Lower values (10-30): More real-time updates, but higher database load
# Higher values (60-120): Less database load, but updates are less frequent
# Default: 60 seconds (1 minute)
# Recommended: 60 seconds for better concurrency when many users are running tasks
monitor_update_interval: 60
```

**系统配置说明**：

- `db`: **全局数据库路径**（重要）
  - 这是实际使用的全局数据库路径，所有 annotask 实例共享此数据库
  - 建议使用绝对路径，确保所有用户都能访问
  - 如果多个用户或多进程需要访问，需要设置相应的文件权限（见下方"全局数据库权限设置"）

- `project`: 默认项目名称，默认为 `anno`

- `retry.max`: 最大重试次数，默认为 3

- `queue`: SGE 默认队列，默认为 `sci.q`

- `sge_project`: SGE 项目名称（用于资源配额管理），默认为空

- `sgeenv`: SGE 环境配置文件路径（可选，仅用于 qsubsge 模式）
  - 如果设置，程序将使用此路径加载 SGE 环境变量
  - 如果未设置，程序将自动检测常见安装路径中的 settings.sh
  - **重要**：此配置项应仅在系统配置文件中设置（不在用户配置中），以便统一管理
  - 所有用户共享此配置，确保使用相同的 SGE 环境
  - 示例：`sgeenv: /opt/gridengine/default/common/settings.sh`

- `node`: 允许使用 qsubsge 模式的节点列表（列表格式，支持多个节点）
  - 如果为空或不设置，则不对 qsubsge 模式做节点限制
  - 如果设置了节点列表，当前节点必须在列表中才能使用 qsubsge 模式
  - 如果当前节点不在允许的列表中，程序会报错退出
  - 示例：
    ```yaml
    node:
      - bj-sci-login1
      - login-0-2
    ```

- `defaults`: 各参数的默认值
  - `line`: 默认行分组数，默认为 1
  - `thread`: 默认并发线程数（注意：命令行参数 `-t/--thread` 的默认值为 10，此配置项仅供参考）
  - `cpu`: 默认 CPU 数量（qsubsge 模式），默认为 1
  - 注意：`mem` 和 `h_vmem` 不在配置文件中设置，必须通过命令行参数 `--mem` 和 `--h_vmem` 显式指定

- `monitor_update_interval`: 全局数据库更新间隔（秒），默认为 60
  - 控制任务状态监控更新全局数据库的频率
  - 较低的值（10-30）：更实时的更新，但数据库负载更高
  - 较高的值（60-120）：数据库负载更低，但更新频率较低
  - 推荐：60 秒，在多个用户同时运行任务时提供更好的并发性能

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

### 如何查找环境变量

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
