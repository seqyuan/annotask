# 代码架构分析报告

## 项目结构

```
annotask/
├── cmd/
│   └── annotask/          # 主程序目录
│       ├── config.go      # 配置管理
│       ├── database.go    # 数据库操作
│       ├── delete.go      # delete 模块实现
│       ├── local.go       # local 模块实现
│       ├── main.go        # 主入口和CLI路由
│       ├── monitor.go     # 任务状态监控
│       ├── qsubsge.go     # qsubsge 模块实现
│       ├── shell.go       # Shell脚本生成
│       ├── stat.go        # stat 模块实现
│       ├── task.go        # 任务执行核心逻辑
│       ├── types.go       # 类型定义和常量
│       └── utils.go       # 工具函数
├── pkg/
│   └── gpool/
│       └── gpool.go       # 并发池实现
├── annotask.yaml.example  # 配置文件示例
├── go.mod                 # Go 模块定义
├── go.sum                 # 依赖校验和
├── INSTALL.md             # 安装说明
├── README.md              # 项目说明
├── UPDATE.md              # 更新日志
├── THREAD_ANALYSIS.md     # 线程分析文档
└── LICENSE                # 许可证

```

## 核心模块说明

### 1. 主入口 (`main.go`)
- **职责**: CLI路由和模块分发
- **功能**:
  - 解析命令行参数
  - 识别模块名称（local, qsubsge, stat, delete）
  - 分发到对应的模块处理函数
  - 显示帮助信息

### 2. 配置管理 (`config.go`)
- **职责**: 配置文件加载和管理
- **功能**:
  - 支持两级配置系统：用户配置（`~/.annotask.yml`）和系统配置（`annotask.yaml`）
  - 配置优先级：命令行参数 > 用户配置 > 系统配置 > 程序默认值
  - 自动创建用户配置文件（首次空运行时）
  - 智能配置合并：用户配置优先于系统配置
  - 提供默认配置值
  - 获取当前用户ID
  - 节点检查（qsubsge模式）：支持多个允许的节点，空列表表示无限制

### 3. 数据库操作 (`database.go`)
- **职责**: 数据库初始化和CRUD操作
- **功能**:
  - 本地数据库（job表）初始化
  - 全局数据库（tasks表）初始化
  - 数据库迁移（添加新列）
  - 任务状态更新
  - 节点信息管理

### 4. 任务执行 (`task.go`)
- **职责**: 任务执行核心逻辑
- **功能**:
  - 本地任务执行 (`RunCommand`)
  - SGE任务提交 (`SubmitQsubCommand`)
  - 退出码检查 (`CheckExitCode`)
  - 任务迭代执行 (`IlterCommand`)

### 5. 模块实现

#### Local 模块 (`local.go`)
- **职责**: 本地并行执行模式
- **功能**:
  - 解析命令行参数
  - 创建任务数据库
  - 启动任务执行循环
  - 管理重试机制
  - 更新全局数据库

#### QsubSGE 模块 (`qsubsge.go`)
- **职责**: SGE集群提交模式
- **功能**:
  - 解析命令行参数（包括CPU、内存、队列、SGE项目等资源）
  - 节点检查：检查当前节点是否在配置文件的允许节点列表中（支持多个节点，空列表表示无限制）
  - 提交任务到SGE
  - 使用DRMAA库与Grid Engine交互
  - 支持多队列（逗号分隔）
  - 支持SGE项目资源配额管理（-P参数）
  - 仅在用户显式设置时使用内存参数（--mem, --h_vmem）
  - 支持两种并行环境模式：
    - pe_smp 模式（默认，`--mode pe_smp`）：使用 `-pe smp X` 指定CPU数量
    - num_proc 模式（`--mode num_proc`）：使用 `-l p=X` 指定CPU数量

#### Stat 模块 (`stat.go`)
- **职责**: 查询任务状态
- **功能**:
  - 从全局数据库查询任务状态
  - 支持按项目筛选（-p参数）
  - 显示任务统计信息
  - 使用-p参数时自动显示shell路径列表
  - 输出格式：无-p时显示所有项目汇总，有-p时显示项目详情和shell路径

#### Delete 模块 (`delete.go`)
- **职责**: 删除任务记录
- **功能**:
  - 从全局数据库删除任务记录
  - 支持按项目和模块筛选

### 6. 监控 (`monitor.go`)
- **职责**: 实时监控任务状态变化
- **功能**:
  - 定期查询数据库
  - 检测状态变化
  - 输出表格格式的状态信息
  - 更新全局数据库记录

### 7. 工具函数

#### Shell 脚本生成 (`shell.go`)
- 生成SGE提交脚本

#### 工具函数 (`utils.go`)
- 时间格式化
- 错误处理
- 计数统计

#### 类型定义 (`types.go`)
- 数据结构定义
- 常量定义
- 枚举类型

## 冗余文件分析

### ✅ 冗余文件已清理

`cmd/annotask/cmd/` 目录已删除（2025-12-09）

**已删除的文件**:
- `cmd/annotask/cmd/local.go`
- `cmd/annotask/cmd/stat.go`
- `cmd/annotask/cmd/delete.go`
- `cmd/annotask/cmd/qsubsge.go`

**原因**:
1. 没有任何代码引用该目录下的文件
2. `main.go` 只调用 `cmd/annotask/` 目录下的函数
3. 该目录下的文件是旧版本备份，内容已过时
4. 编译时不会包含这些文件（Go只编译同一包下的文件）

## 依赖关系

```
main.go
  ├── config.go (LoadConfig)
  ├── local.go (runLocalMode)
  ├── qsubsge.go (runQsubSgeMode)
  ├── stat.go (RunStatModule)
  └── delete.go (RunDeleteModule)

local.go / qsubsge.go
  ├── database.go (数据库操作)
  ├── task.go (任务执行)
  └── monitor.go (状态监控)

task.go
  ├── shell.go (脚本生成)
  └── database.go (数据库操作)

monitor.go
  ├── database.go (查询任务状态)
  ├── utils.go (时间格式化)
  └── config.go (获取配置)

stat.go / delete.go
  └── database.go (查询/删除记录)
```

## 外部依赖

- `github.com/akamensky/argparse` - 命令行参数解析
- `github.com/mattn/go-sqlite3` - SQLite3数据库驱动
- `github.com/dgruber/drmaa` - DRMAA库（SGE支持）
- `gopkg.in/yaml.v3` - YAML配置文件解析
- `pkg/gpool` - 内部并发池实现

## 代码统计

- **Go文件总数**: 14个
  - 主程序: 13个（cmd/annotask/）
  - 公共包: 1个（pkg/gpool/）

- **所有文件均为必需文件，无冗余**

## 建议

1. ✅ **冗余文件已清理**: `cmd/annotask/cmd/` 目录已删除
2. **保持当前架构**: 模块化设计清晰，职责分离明确
3. **文档同步**: README 和 UPDATE.md 已反映当前架构

