# 任务状态查询

`annotask stat` 命令用于查询任务状态。该命令会自动从本地数据库读取最新任务状态并更新到全局数据库，确保显示的信息是最新的。

## 基本用法

### 查询所有任务

```bash
annotask stat
```

显示所有项目的任务汇总信息。

**示例输出**：
```
project          module               mode       status     statis          stime        etime       
myproject        input                local      -          5/5             12-25 14:30  12-25 15:45
myproject        process              qsubsge    -          14/16           12-26 09:15  -
testproject      analysis             local      -          8/8             12-24 10:20  12-24 11:30
```

**输出说明**：
- `project`: 项目名称
- `module`: 模块名称（基于输入文件名）
- `mode`: 执行模式（local 或 qsubsge）
- `status`: 任务状态（可能为 "-"）
- `statis`: 任务统计，格式为 `已完成数/总任务数`（例如：`14/16` 表示总共16个任务，14个已完成）
- `stime`: 开始时间（格式：MM-DD HH:MM）
- `etime`: 结束时间（格式：MM-DD HH:MM，未结束显示 "-"）

### 查询特定项目

```bash
annotask stat -p myproject
```

显示指定项目的详细任务信息。

**示例输出**：
```
id     module               pending  running  failed   finished  stime        etime       
1      input                0        0        0        5         12-25 14:30  12-25 15:45
2      process              2        3        1        10        12-26 09:15  -

1 /absolute/path/to/input.sh
2 /absolute/path/to/process.sh
```

**输出说明**：
- 第一部分：任务状态表格
  - `id`: 任务ID（数据库中的主键）
  - `module`: 模块名称
  - `pending`: 待处理任务数
  - `running`: 运行中任务数
  - `failed`: 失败任务数
  - `finished`: 已完成任务数
  - `stime`: 开始时间（格式：MM-DD HH:MM）
  - `etime`: 结束时间（格式：MM-DD HH:MM，未结束显示 "-"）
- 第二部分：任务ID和shell路径列表（空行分隔）
  - 格式：`id 完整shell路径`
  - 每个模块对应一行，用于快速定位任务文件

## 参数说明

```
-h, --help        Print help information
-p, --project     Filter by project name
```

## 工作原理

1. **自动更新**：`stat` 命令会自动从本地数据库（`{输入文件路径}.db`）读取最新任务状态，并更新到全局数据库
2. **状态同步**：确保全局数据库中的任务状态与本地数据库保持一致
3. **实时查询**：每次运行 `stat` 命令时，都会重新同步状态，确保显示的信息是最新的

## 使用示例

```bash
# 查看所有项目的任务状态
annotask stat

# 查看特定项目的任务状态
annotask stat -p myproject

# 查看另一个项目的任务状态
annotask stat -p testproject
```

## 注意事项

- `stat` 命令会读取本地数据库并更新全局数据库，如果本地数据库不存在或无法访问，该任务的状态可能不会更新
- 任务ID（`id`）可用于 `delete` 命令的 `-k/--id` 参数，快速删除特定任务记录
- 时间格式为 "MM-DD HH:MM"，便于快速查看任务执行时间

