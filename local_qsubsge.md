# 本地与集群模式

`annotask` 支持两种运行模式：本地并行模式（local）和 SGE 集群模式（qsubsge）。

## 运行模式

1. **local 模式**（默认）：在本地并行执行任务
2. **qsubsge 模式**：将任务投递到 SGE 集群执行

## local 模式

### 基本用法

```bash
annotask -i input.sh -l 2 -t 4 --project myproject
```

或者显式指定模式：

```bash
annotask local -i input.sh -l 2 -t 4 --project myproject
```

### 参数说明

```
-i, --infile    输入文件，shell脚本（必需）
-l, --line      每几行作为一个任务单元（默认：1）
-t, --thread    最大并发任务数（默认：10）
    --project   项目名称（默认：从用户配置或系统配置读取）
```

### 使用示例

```bash
annotask -i input.sh -l 2 -t 2 --project test
```

**标准错误流的输出**：

```
[1 2 3 4 5]
All works: 5
Successed: 3
Error: 2
Err Shells:
2	/Volumes/RD/parrell_task/input.sh.shell/task_0002.sh
3	/Volumes/RD/parrell_task/input.sh.shell/task_0003.sh
```

### 运行产生的目录结构

```
.
├── input.sh
├── input.sh.db
├── input.sh.log
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

**文件说明**：
- `input.sh.db`：本地任务数据库（SQLite）
- `input.sh.log`：实时监控日志文件（文件开头会记录执行的命令）
- `input.sh.shell/`：子脚本存放目录
- `task_XXXX.sh`：子脚本文件
- `task_XXXX.sh.o`：标准输出文件
- `task_XXXX.sh.e`：标准错误文件
- `task_XXXX.sh.sign`：成功标记文件（任务成功完成后自动创建）

## qsubsge 模式

### 基本用法

```bash
# 只设置 mem，DRMAA 投递时只使用 -l vf=XG（虚拟内存）
annotask qsubsge -i input.sh -l 2 -t 4 --project myproject --cpu 2 --mem 4

# 只设置 h_vmem，DRMAA 投递时只使用 -l h_vmem=XG（硬虚拟内存限制）
annotask qsubsge -i input.sh -l 2 -t 4 --project myproject --cpu 2 --h_vmem 8

# 同时设置 mem 和 h_vmem
annotask qsubsge -i input.sh -l 2 -t 4 --project myproject --cpu 2 --mem 4 --h_vmem 8

# 指定队列（单个或多个，逗号分隔）
annotask qsubsge -i input.sh --queue sci.q
annotask qsubsge -i input.sh --queue trans.q,nassci.q,sci.q

# 指定 SGE 项目（用于资源配额管理）
annotask qsubsge -i input.sh -P bioinformatics

# 指定节点（单个节点）
annotask qsubsge -i input.sh --hostname node1

# 指定节点（多个节点，任选其一）
annotask qsubsge -i input.sh --hostname node1,node2,node3

# 使用 -l p=X 并行环境模式（默认）
annotask qsubsge -i input.sh --cpu 4 --h_vmem 18
# 或显式指定
annotask qsubsge -i input.sh --cpu 4 --h_vmem 18 --mode num_proc

# 使用 -pe smp 模式
annotask qsubsge -i input.sh --cpu 4 --h_vmem 5 --mode pe_smp
```

### 参数说明

```
-i, --infile    输入文件，shell脚本格式（必需）
-l, --line      每几行作为一个任务单元（默认：从用户配置或系统配置读取）
-t, --thread    最大并发任务数（默认：10）
    --project   项目名称（默认：从用户配置或系统配置读取）
    --cpu       CPU数量（默认：从用户配置或系统配置读取）
    --mem       虚拟内存（vf）大小（GB，映射到 -l vf=XG，仅在显式设置时在DRMAA中使用）
    --h_vmem    硬虚拟内存限制（h_vmem）大小（GB，映射到 -l h_vmem=XG，仅在显式设置时在DRMAA中使用）
    --queue     队列名称（多个队列用逗号分隔，默认：从用户配置或系统配置读取）
    -P, --sge-project  SGE项目名称（用于资源配额管理，默认：从用户配置或系统配置读取）
    --hostname  指定节点（单个节点或逗号分隔的多个节点，映射到 -l h=hostname，仅 qsubsge 模式）
    --mode      并行环境模式：num_proc（使用 -l p=X，默认）或 pe_smp（使用 -pe smp X）
```

**重要说明**：
- `--mem` 和 `--h_vmem` 参数只有在用户显式设置时，才会在 DRMAA 投递时使用
- `--mem` 对应 SGE 的 `vf` 资源（虚拟内存），DRMAA 投递时使用 `-l vf=XG`
- `--h_vmem` 对应 SGE 的 `h_vmem` 资源（硬虚拟内存限制），DRMAA 投递时使用 `-l h_vmem=XG`
- 如果只设置了 `--mem`，DRMAA 投递时只包含 `-l vf=XG`，不包含 `-l h_vmem`
- 如果只设置了 `--h_vmem`，DRMAA 投递时只包含 `-l h_vmem=XG`，不包含 `-l vf`
- 如果都不设置，DRMAA 投递时不会包含内存相关参数
- `--queue` 支持多个队列，用逗号分隔（例如：`trans.q,nassci.q,sci.q`）
- `-P/--sge-project` 用于 SGE 资源配额管理，如果未设置或设置为 "none"（大小写不敏感），则不在 DRMAA 中使用 `-P` 参数
- `--hostname` 用于指定任务运行的节点，支持单个节点或逗号分隔的多个节点（例如：`node1` 或 `node1,node2`），DRMAA 投递时使用 `-l h=hostname`。如果设置为 "none"（大小写不敏感）或空，则不会添加节点限制。**注意：此参数仅对 qsubsge 模式有效，local 模式不支持**

**并行环境模式**：
- **num_proc 模式**（默认，`--mode num_proc`）：使用 `-l p=X` 指定 CPU 数量
  - 示例：`--cpu 4 --h_vmem 18` → `-l h_vmem=18G,p=4`
  - 这里的内存指的是总内存
- **pe_smp 模式**（`--mode pe_smp`）：使用 `-pe smp X` 指定 CPU 数量
  - 示例：`--cpu 4 --h_vmem 5 --mode pe_smp` → `-l h_vmem=5G -pe smp 4`
  - 这里的内存指的是单 CPU 需要消耗的内存

### 注意事项

- qsubsge 模式会检查当前节点是否在配置文件中的 `node` 列表中，以防止在计算节点误投递任务
- 如果配置文件中设置了 `node` 列表，当前节点必须在列表中才能使用 qsubsge 模式
- 如果 `node` 为空或不设置，则不对节点做限制
- 如果当前节点不在允许的列表中，程序会报错退出
- 任务会自动投递到 SGE 集群，输出文件会生成在子脚本所在目录（`{输入文件路径}.shell`）
- 输出文件格式为 `task_0001.sh.o.{jobID}` 和 `task_0001.sh.e.{jobID}`
- 例如：输入文件为 `input.sh`，子任务为 `task_0001.sh`，则输出文件为：
  - `input.sh.shell/task_0001.sh.o.{jobID}`（标准输出）
  - `input.sh.shell/task_0001.sh.e.{jobID}`（标准错误）

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

### -t 参数说明

如果要对整个annotask程序所在进程的资源做限制，可设置`-t`参数，指定最多同时并行多少个子进程。如果不设置，默认值为 10。

### annotask产生的文件

1. `input.sh.db`文件，此文件为sqlite数据库（本地任务数据库）
2. `input.sh.shell`目录，子脚本存放目录
3. 按照`-l`参数切割的input.sh的子脚本，存放在`input.sh.shell`目录
4. 子脚本命名格式：`task_0001.sh`（固定使用 `task` 作为前缀，最多支持9999个子任务）
5. 每个子脚本的标准输出和标准错误会分别保存到 `.o` 和 `.e` 文件

## 实时监控

annotask在运行时会启动一个独立的goroutine实时监控任务状态，并将状态变化以表格格式输出到日志文件。日志文件位置为 `{输入文件路径}.log`（例如：`input.sh.log`）。

### 日志文件格式

日志文件开头会记录执行的命令，格式如下：

```
annotask qsubsge -i input.sh --cpu 4 --h_vmem 5 --hostname node1

try    task   status     taskid     exitcode time        
1:3    0001   Running    3652318    -        12-09 10:24 
```

如果多次运行同一个任务，每次运行会在日志文件末尾追加新的命令和监控信息（前面会有一个空行分隔）。

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
- 监控输出写入到日志文件 `{输入文件路径}.log`（例如：`input.sh.log`），而不是标准输出
- 表头只在第一次输出时显示一次
- Pending 状态的任务不会显示在监控输出中
- 时间格式为"月-日 时:分"（例如：`12-09 10:24`）
- 默认更新间隔为 60 秒，可通过配置文件中的 `monitor_update_interval` 参数自定义

### 查看监控日志

```bash
# 实时查看监控日志
tail -f input.sh.log

# 查看最近的监控日志
tail -n 100 input.sh.log
```

## 自动重试机制

### 重试策略

- 失败的任务会自动重试，最多重试3次（可在配置文件中修改）
- 重试次数记录在数据库的`retry`列中
- 只有 qsubsge 模式支持自动重试，local 模式不支持自动重试

### 重新运行时的 retry 重置

当第一次运行部分任务失败后，第二次重新运行 `annotask local` 或 `annotask qsubsge` 时：
- 失败任务的 `retry` 值会被重置为 1
- 这样可以确保重新运行的任务从第一轮重试开始，而不是继续之前的重试计数
- 适用于 local 和 qsubsge 两种模式

### 内存自适应重试

在qsubsge模式下，如果任务因为内存不足被kill（退出码137或错误日志中包含内存相关关键词），annotask会自动：

1. 检测内存错误
2. 根据用户设置的参数，只增加相应参数的内存（向上取整）：
   - 如果用户只设置了 `--mem`，只增加 `mem`（增加125%，向上取整）
   - 如果用户只设置了 `--h_vmem`，只增加 `h_vmem`（增加125%，向上取整）
   - 如果用户同时设置了两个参数，两个都增加（增加125%，向上取整）
   - 如果用户都没有设置，不进行内存增加
3. 重新投递任务

## 其他使用方式

```bash
annotask -i input.sh -l 2 -t 2 --project test
```

我们可以把以上命令写入到`work.sh`里，然后把`work.sh`投递到SGE或者K8s计算节点。

