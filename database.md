# 数据库结构

`annotask` 使用 SQLite 数据库记录任务状态。数据库分为两类：本地任务数据库和全局任务数据库。

## 本地任务数据库（input.sh.db）

annotask会针对每一个输入脚本，在脚本所在目录生成`脚本名称`+`.db`的sqlite3数据库，用于记录各`子脚本`的运行状态，例如`input.sh`对应的数据库名称为`input.sh.db`。

### 数据库表结构

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
node        TEXT                               # 执行节点（qsubsge模式）
```

### 字段说明

- **subJob_num**：子任务编号，表示记录的是第几个子脚本
- **shellPath**：对应子脚本路径
- **status**：对应子脚本的状态，状态有4种：
  - `Pending`：待处理
  - `Running`：运行中
  - `Failed`：失败
  - `Finished`：已完成
- **exitCode**：对应子脚本的退出码
  - `0`：成功
  - 非`0`：失败
- **retry**：如果子脚本出错的情况下annotask程序自动重新尝试运行该出错子进程的次数（最多3次，可在配置文件中自定义）
- **starttime**：子脚本开始运行的时间
- **endtime**：子脚本结束运行的时间
- **mode**：运行模式：`local` 或 `qsubsge`
- **cpu**：CPU数量（qsubsge模式）
- **mem**：虚拟内存（vf）大小（GB，qsubsge模式，映射到 `-l vf=XG`，仅在用户显式设置时使用）
- **h_vmem**：硬虚拟内存限制（h_vmem）大小（GB，qsubsge模式，映射到 `-l h_vmem=XG`，仅在用户显式设置时使用）
- **taskid**：任务ID
  - local模式：存储进程PID
  - qsubsge模式：存储SGE Job ID
- **node**：执行节点（qsubsge模式），记录任务在哪个计算节点上执行

## 全局任务数据库（annotask.db）

annotask会在程序所在目录创建全局数据库`annotask.db`（路径可在配置文件中修改），用于记录所有任务的总体状态。

### 数据库表结构

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
status          TEXT DEFAULT 'running'           # 任务状态（running/completed/failed）
node            TEXT                             # 执行节点
pid             INTEGER                          # 主进程PID
UNIQUE(usrID, project, module, starttime)
```

### 字段说明

- **Id**：自增主键
- **usrID**：用户ID，用于区分不同用户的任务
- **project**：项目名称，用于组织和管理任务
- **module**：模块名称（输入文件basename，不含扩展名）
- **mode**：运行模式（`local` 或 `qsubsge`）
- **starttime**：任务启动时间
- **endtime**：任务结束时间（未结束时为NULL）
- **shellPath**：输入文件完整路径
- **totalTasks**：子任务总数
- **pendingTasks**：待处理任务数
- **failedTasks**：失败任务数
- **runningTasks**：运行中任务数
- **finishedTasks**：已完成任务数
- **status**：任务状态
  - `running`：运行中
  - `completed`：已完成（所有子任务成功）
  - `failed`：失败（至少有一个子任务失败）
- **node**：执行节点
  - local模式：主机名
  - qsubsge模式：计算节点名称
- **pid**：主进程PID（用于删除运行中的任务时终止进程）

### 唯一约束

`tasks` 表有一个唯一约束：`UNIQUE(usrID, project, module, starttime)`，确保同一用户、同一项目、同一模块、同一启动时间的任务记录唯一。

## 数据库关系

- **本地数据库**：每个输入文件对应一个本地数据库，记录该输入文件的所有子任务状态
- **全局数据库**：所有输入文件共享一个全局数据库，记录所有任务的总体状态
- **状态同步**：`stat` 命令会自动从本地数据库读取最新状态并更新到全局数据库

## 数据库访问

### 本地数据库

- 位置：`{输入文件路径}.db`（例如：`input.sh.db`）
- 访问：每个输入文件独立访问，互不影响
- 用途：记录子任务状态，支持断点续传

### 全局数据库

- 位置：配置文件中的 `db` 字段指定（系统配置文件中的 `db` 路径）
- 访问：所有用户和进程共享访问
- 用途：记录所有任务的总体状态，支持任务查询和管理
- 权限：如果多个用户或多进程需要访问，需要设置相应的文件权限（详见 [INSTALL.md](INSTALL.md)）

## 数据库操作

### 查询任务状态

```bash
# 查询所有任务
annotask stat

# 查询特定项目
annotask stat -p myproject
```

`stat` 命令会自动从本地数据库读取最新状态并更新到全局数据库。

### 删除任务记录

```bash
# 删除整个项目
annotask delete -p myproject

# 删除特定模块
annotask delete -p myproject -m input

# 按任务ID删除
annotask delete -k 1
```

`delete` 命令会从全局数据库删除任务记录，但不会删除本地数据库和任务文件。

## 数据库维护

### 备份

建议定期备份全局数据库：

```bash
# 备份全局数据库
cp /path/to/annotask.db /path/to/annotask.db.backup
```

### 清理

如果数据库文件过大，可以删除已完成的项目记录：

```bash
# 删除已完成的项目
annotask delete -p completed_project
```

### 权限设置

如果多个用户需要访问全局数据库，需要设置相应的文件权限（详见 [INSTALL.md](INSTALL.md)）。

