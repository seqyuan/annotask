# 安装说明

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
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.8.5
```

**优点**：编译后的二进制文件可直接运行，不需要运行时设置环境变量或包装脚本。

### 方法 2：使用运行时包装脚本

如果不使用 rpath，需要在运行时设置 `LD_LIBRARY_PATH`。创建一个包装脚本（例如 `/home/seqyuan/go/bin/annotask`）：

```bash
# 编译时
export CGO_CFLAGS="-I/opt/gridengine/include"
export CGO_LDFLAGS="-L/opt/gridengine/lib/lx-amd64 -ldrmaa"
export LD_LIBRARY_PATH=/opt/gridengine/lib/lx-amd64:$LD_LIBRARY_PATH
CGO_ENABLED=1 go install github.com/seqyuan/annotask/cmd/annotask@v1.8.5
```

```bash
mv /home/seqyuan/go/bin/annotask /home/seqyuan/go/bin/annotask_linux
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



