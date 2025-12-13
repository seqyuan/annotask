# 删除任务记录

`annotask delete` 命令用于从全局数据库删除任务记录。支持按项目、模块或任务ID删除。

## 基本用法

### 删除整个项目

```bash
annotask delete -p myproject
```

删除指定项目的所有任务记录。

### 删除特定模块

```bash
annotask delete -p myproject -m input
```

删除指定项目中特定模块的任务记录。

### 按任务ID删除

```bash
annotask delete -k 1
```

删除指定任务ID的记录（任务ID可通过 `annotask stat -p project` 命令查看）。

## 参数说明

```
-h, --help        Print help information
-p, --project     Project name (required when not using -k/--id)
-m, --module      Module (shell path basename without extension)
-k, --id          Task ID (from stat -p output)
```

**参数说明**：
- `-p/--project`: 项目名称。当不使用 `-k/--id` 时，此参数为必需
- `-m/--module`: 模块名称（输入文件 basename，不含扩展名）。可选，用于删除特定模块
- `-k/--id`: 任务ID。使用此参数时，不需要指定 `-p` 和 `-m` 参数

## 删除行为

### 对于运行中的任务

如果删除的任务状态为 `running`，`delete` 命令会执行以下操作：

1. **终止主进程**：如果主进程（PID）存在，会终止主进程及其所有子进程
2. **处理子任务**：
   - **qsubsge 模式**：使用 `qdel` 命令终止所有运行中的 SGE 作业，并将状态更新为 `failed`
   - **local 模式**：将本地数据库中所有运行中的任务状态更新为 `failed`
3. **删除记录**：从全局数据库中删除任务记录

### 对于非运行中的任务

对于非运行中的任务（已完成、失败等），`delete` 命令会直接从全局数据库删除记录，不进行进程终止操作。

## 使用示例

```bash
# 删除整个项目
annotask delete -p myproject

# 删除特定模块
annotask delete -p myproject -m input

# 按任务ID删除（任务ID可通过 stat -p 查看）
annotask delete -k 1

# 查看帮助信息
annotask delete --help
```

## 输出示例

### 删除整个项目

```bash
$ annotask delete -p myproject
Terminated main process (PID: 12345) and its children for module 'input'
Terminated SGE job 8944790 (task 1) using qdel
Terminated SGE job 8944791 (task 2) using qdel
Updated task 3 status to Failed
Deleted 2 task record(s) for project 'myproject'
```

### 删除特定模块

```bash
$ annotask delete -p myproject -m input
Deleted 1 task record(s) for project 'myproject' and module 'input'
```

### 按任务ID删除

```bash
$ annotask delete -k 1
Deleted 1 task record(s) with ID 1
```

## 注意事项

1. **删除操作不可逆**：删除任务记录后，无法恢复。请谨慎操作。

2. **运行中任务的终止**：
   - 对于运行中的任务，`delete` 会尝试终止相关进程和作业
   - 如果进程或作业已经结束，不会报错，会静默跳过

3. **本地数据库**：`delete` 命令只删除全局数据库中的记录，不会删除本地数据库（`{输入文件路径}.db`）和任务文件

4. **权限要求**：
   - 删除任务记录需要能够访问全局数据库
   - 终止进程需要相应的系统权限

5. **任务ID获取**：任务ID可以通过 `annotask stat -p project` 命令查看，输出中的 `id` 列即为任务ID

## 常见使用场景

1. **清理已完成的项目**：项目完成后，删除项目记录以清理数据库
   ```bash
   annotask delete -p completed_project
   ```

2. **删除失败的任务**：删除失败的任务记录，准备重新运行
   ```bash
   annotask delete -p myproject -m failed_module
   ```

3. **快速删除特定任务**：使用任务ID快速删除
   ```bash
   annotask stat -p myproject  # 查看任务ID
   annotask delete -k 1        # 删除任务ID为1的记录
   ```

