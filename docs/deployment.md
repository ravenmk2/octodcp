# 安装与容器部署设计

本文描述一期目标状态，CLI、配置格式和容器片段均为设计示例，**当前仓库尚未实现这些命令与安装逻辑**。当前程序直接启动 HTTP 服务，Dockerfile 也尚未设置这里的目录和运行用户。整体结构见 [架构](architecture.md)。

## 1. 目录与运行边界

应用默认工作目录为进程 cwd，数据目录默认 `<cwd>/data/`。启动时解析为绝对路径，后续不受工作目录变化影响。容器使用 `/app/` 为工作目录，故默认数据目录固定为 `/app/data/`。相对 `--data-dir` 按启动 cwd 解析，不按二进制所在目录猜测。

```text
/app/
  octodcp                       # 单个二进制，包含 SPA 和迁移
  data/                         # 唯一应用持久化挂载点
    config.yaml                 # 部署配置；数据库口令为密文
    master.key                  # 32 字节随机主密钥，安装后不得自动重建
    installation.json           # 本地完成标记，含格式版本、安装 ID、完成时间
    setup.token                 # 仅首次安装期间存在的一次性维护者令牌
    locks/
      instance.lock             # 进程持有的操作系统锁，文件存在不代表锁有效
      install.lock              # 安装临界区锁
    .install/
      staging.json              # 中断恢复检查点，只有标识和阶段，不含明文口令
      config.pending.yaml       # 同样只保存加密口令；完成后原子替换正式文件
    media/
      avatars/
        ab/<random>.png         # 由服务端生成的相对路径
    tmp/
      uploads/                  # 同一文件系统中暂存规范化图像
```

二进制、前端和迁移不放到数据挂载里。MySQL 数据由数据库服务自己管理，不能在应用数据卷中夹放 `/data/mysql`。应用日志写 stdout/stderr，不默认在数据目录创建日志文件。

Unix 下配置、主密钥、安装令牌及恢复文件权限为 0600，目录为 0700；容器数据目录归运行用户所有。Windows 使用当前部署账号的受限 ACL，不能把 Unix mode 当作已经限制 Windows 访问。实现时在仓库忽略 `/data/`，避免开发数据及密钥入库。

一期一个应用实例持有 `instance.lock`。同一数据目录的第二个进程拒绝启动；不同本地卷也不能作为同一系统的多副本部署方式。维护命令要求先停止服务，再独占数据目录。

## 2. 配置职责

配置优先级为显式 CLI 参数 > 环境变量 > `data/config.yaml` > 默认值。只对实际声明的配置键应用这一规则；安装 ID、主密钥来源及安装完成状态不能靠任意环境变量绕过校验。运行时不打印配置对象或包含口令的 DSN。

| 配置 | 位置 / 建议环境变量 | 说明 |
| --- | --- | --- |
| 数据目录 | CLI `--data-dir` / `OCTODCP_DATA_DIR` | 默认 cwd 下 data；在读取配置文件之前确定 |
| 监听地址 | `server.listen` / `OCTODCP_LISTEN` | 默认 `0.0.0.0:8080` |
| 公开地址 | `server.public_url` / `OCTODCP_PUBLIC_URL` | 必须为无用户信息、查询和片段的绝对 HTTP(S) 来源地址；一期部署在根路径 |
| 可信反代 | `server.trusted_proxies` / `OCTODCP_TRUSTED_PROXIES` | 默认空数组；环境变量采用逗号分隔 CIDR |
| Cookie 安全属性 | 从公开地址派生 | HTTPS 必须 Secure；HTTP 仅用于本地预览或维护者明确选择的内网部署 |
| 数据库连接 | `database.*` | 安装向导保存；密码为主密钥加密值 |
| 数据库私有 CA | `database.ca_file` / `OCTODCP_DATABASE_CA_FILE` | 可选部署文件路径；用于 verify_identity，不由浏览器指定路径 |
| 安装令牌 | `OCTODCP_SETUP_TOKEN` 或自动生成文件 | 仅未安装阶段有效，不写入正式配置 |

未安装时公开地址可以由环境变量预置，未配置则仅以 `http://localhost:8080` 作为本地引导默认值；远程部署必须设置实际公开地址。安装向导只读展示该地址，来源与 CSRF 校验以它为准，不从任意 Host / Forwarded 请求头自动推导可信地址。

配置示例，不含可使用的口令或主密钥：

```yaml
schema_version: 1
installation_id: "<32 位小写十六进制安装 ID>"
server:
  listen: "0.0.0.0:8080"
  public_url: "https://octodcp.example.com"
  trusted_proxies: []
database:
  host: "mysql"
  port: 3306
  name: "octodcp"
  username: "octodcp"
  password: "enc:v1:<base64url nonce 与密文>"
  tls_mode: "disabled"
  ca_file: ""
```

业务设置（系统名称、时区、注册开关、会话有效期）只在 MySQL `system_setting` 受控 KV，不与 YAML 同时维护。安装创建注册表要求的全部键；后续新增键由迁移补齐，启动检查必需键的类型和范围，缺失或无效时进入维护状态，不静默替换成默认值。没有邮箱、SMTP 或邮件相关配置。系统设置界面仅展示部署公开地址，不修改它。

主密钥使用 AES-256-GCM 保护敏感配置；密文包含格式版本和随机 nonce，AAD 绑定安装 ID 与字段用途。启动先读取密钥、校验本地记录，再解密数据库口令。丢失主密钥时不能生成一把新密钥后继续尝试旧密文。

## 3. 启动状态

| 状态 | 条件 | 行为 |
| --- | --- | --- |
| `setup_required` | 未发现安装标记、密钥、配置、中断检查点或非空业务文件；空目录及安装令牌/锁文件可以存在 | 提供安装界面，需维护者令牌才能初始化 |
| `initializing` | 本进程正在持有安装锁执行安装 | 禁止第二次安装，显示初始化中 |
| `ready` | 配置、密钥、本地标记、数据库安装 ID/密钥指纹与迁移版本全部匹配 | 开放业务 API |
| `recovery_required` | 缺少部分本地文件、有安装中断痕迹、ID/密钥指纹冲突或数据库出现无法解释的部分结构 | 停止业务，提示部署维护者本地检查，不能从页面重新安装 |
| `unavailable` | 已有配置但数据库不可达、服务暂不可用或迁移版本不兼容 | 停止业务，保留原安装状态，不出现新安装向导 |

存在旧配置但数据库为空也不能视为全新部署；这是需要恢复或显式处理的状态。数据库连通但迁移版本不兼容时提供通用维护提示，详细版本诊断只给维护者日志或命令。

`GET /healthz` 保持存活检查，进程能够处理 HTTP 时为 200；不承诺数据库可用。新增 `GET /readyz` 在 `ready` 或 `setup_required` 返回 200，允许安装界面被访问，其余状态为 503。检查内容不返回密钥、路径或连接细节。探针的这个定义意味着“可以接受当前阶段的 HTTP 流量”，部署监控另从 setup 状态区分是否已安装。

## 4. 安装所有权与数据库要求

未配置 `OCTODCP_SETUP_TOKEN` 时，应用生成至少 32 随机字节的安装令牌，写入 `data/setup.token`，日志只告知文件位置，不直接打印令牌。维护者读取文件后在安装页交换短期安装 Cookie。页面及日志不展示数据库已有账号或密钥信息。

安装前部署者准备 MySQL 8+ 的专用空 schema 和具有初始化权限的账号；当前最低为 8.0.16，拒绝不会执行 CHECK 的更早版本，不限于 8.4。后续主版本需通过迁移兼容验证，不能只比较版本号大小。应用不用 MySQL root，不在不明的现有 schema 上直接套表。检查版本、schema 是否为空、字符集及必要权限，空 schema 不含其他业务表。连接检查不建库、不写业务数据；安装执行时再次检查，防止检查后发生变化。

安装临界区同时使用数据目录操作系统锁和 MySQL `GET_LOCK` 命名锁。数据库锁名使用目标 schema 的固定摘要，长度不超过 64 字节；专用连接持有至安装结束并显式释放。它不是 SQL 事务锁，不能假定事务提交会释放它。多个实例指向同一空 schema 时，后取得锁的一方重新检查安装记录与 schema，不能继续覆盖。

## 5. 安装步骤与恢复

这些步骤有先后顺序，不能声称跨文件系统与 MySQL 整体原子提交：

1. 验证安装会话、CSRF、输入及数据库目标；取得两类安装锁，再检查仍未安装。
2. 生成安装 ID 和唯一主密钥。将密钥、含加密数据库口令的 pending 配置和检查点写入同一数据目录，使用临时文件、fsync 与原子重命名；恢复后必须复用这一把密钥。
3. 执行对应版本的 goose 初始迁移。MySQL DDL 隐式提交，此阶段不与创建账号放进同一个事务。
4. 一个业务事务内创建创始人、凭据、默认设置、`system_instance` 和安装完成审计。安装 ID 与主密钥指纹必须匹配已落盘内容。
5. 业务事务提交成功后原子就位正式配置，最后写 `installation.json` 完成标记；重新核验本地与数据库一致，进入 ready。
6. 删除安装令牌和已完成的检查点，销毁安装会话及临时 CSRF 密钥。即使清理令牌文件失败，ready 状态也必须拒绝所有安装写操作。

磁盘写入必须先于种子事务提交得到持久性保证；标记必须晚于提交。容器被终止后，完成标记缺失不等于数据库未初始化。浏览器若未收到最终响应，先读 `/setup/status`；已 ready 就去登录，不能重复创建创始人。

恢复命令的目标是核对已有检查点并完成可证明安全的步骤：

| 中断位置 | 处理原则 |
| --- | --- |
| 本地暂存完成，数据库未迁移 | 验证同一安装 ID、密钥及空 schema 后继续 |
| 部分 DDL 已提交 | 检查 goose 版本及实际结构；只继续已能证明一致的迁移，不盲目重放整个 SQL 文件 |
| 迁移完成，种子未提交 | 验证本次安装拥有的结构，再原子创建种子 |
| 种子已提交，本地标记未完成 | 数据库安装 ID 与主密钥指纹均匹配时，完成 pending 配置与标记，不重建用户或密钥 |
| 记录不匹配、结构无法解释 | 停止自动恢复，提供脱敏诊断；由维护者修复或明确改用新的空 schema |

不提供自动 DROP、清空数据目录、覆盖陌生 schema、重新生成主密钥的恢复捷径。检查点只是证据之一，不能仅因文件写着某个阶段就跳过数据库校验。

## 6. CLI 目标

下列为未来接口形式，尚不可直接运行：

```text
octodcp serve --data-dir /app/data
octodcp db migrate --data-dir /app/data
octodcp setup recover --data-dir /app/data
octodcp admin reset-password --data-dir /app/data --username raven
octodcp config set-database --data-dir /app/data
```

无子命令可作为 serve 的兼容入口。维护命令须先停止服务，独占数据目录；恢复和改密不通过网页提供免认证入口。密码通过交互式隐藏输入读取，不放在命令行参数或普通日志中。恢复账号必须已存在、未删除，不改变其身份或禁用状态。

本地重置在同一事务中修改凭据、递增认证及编辑版本、撤销全部会话、设置下次改密标记，并记录 `actor_type=system` 的 `user.password_recovered` 审计。它可帮助创始人恢复密码，但不能弥补主密钥或数据库丢失。

迁移可选用维护者提供的专用 DDL 账号，用户名/口令从交互输入或临时 `OCTODCP_MIGRATION_USERNAME`、`OCTODCP_MIGRATION_PASSWORD` 读取，口令输入隐藏，不覆盖正式运行配置。安装后可停服通过 `config set-database` 交互式切换为仅有业务读写权限的账号：验证新连接的安装 ID 与主密钥指纹，沿用已有主密钥加密口令，再原子替换配置。不接受指向其他系统或空数据库的连接，不要求维护者手工生成密文。

## 7. 容器目标

示例仅说明最终镜像结构。二进制先在构建流水线编译，运行镜像不包含 Node、Go 工具链或源码。

```dockerfile
FROM alpine:3
RUN apk add --no-cache ca-certificates \
    && addgroup -g 10001 octodcp \
    && adduser -D -u 10001 -G octodcp octodcp \
    && mkdir -p /app/data \
    && chown -R octodcp:octodcp /app
WORKDIR /app
COPY --chown=octodcp:octodcp octodcp /app/octodcp
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/octodcp"]
CMD ["serve"]
```

正式交付时锁定基础镜像版本/摘要，并嵌入 Go 时区数据。只读根文件系统仍需允许写入 `/app/data`；宿主机绑定目录预先由维护者创建并赋予 UID 10001 写权限，不靠应用提权 chown 整个宿主机挂载。

本地部署布局示例：

```text
deployment/
  compose.yaml
  .env                          # 部署环境值，不进 Git
  data/                         # 绑定到应用 /app/data/
MySQL named volume              # 独立挂载到数据库 /var/lib/mysql/
```

```yaml
services:
  app:
    image: ${OCTODCP_IMAGE:-octodcp:phase-a}
    working_dir: /app
    user: "10001:10001"
    read_only: true
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      OCTODCP_PUBLIC_URL: http://localhost:8080
    volumes:
      - ./data:/app/data
    depends_on:
      - mysql
  mysql:
    image: mysql:8.4
    environment:
      MYSQL_DATABASE: octodcp
      MYSQL_USER: octodcp
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:?set MYSQL_PASSWORD}
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:?set MYSQL_ROOT_PASSWORD}
    volumes:
      - mysql-data:/var/lib/mysql
volumes:
  mysql-data:
```

`octodcp:phase-a` 是待构建镜像名，不代表已发布镜像。此例仅绑定本机 HTTP；安装向导填入 mysql:3306、octodcp 数据库和部署者设置的应用数据库密码。`depends_on` 不代表数据库已就绪，应用应对初始连接做有限退避并正确返回未就绪状态。正式部署可由反代终止 HTTPS，公开地址设为真实域名，仅信任反代实际网段，不暴露 MySQL 端口。

## 8. 备份、升级与恢复验收

备份单位为 MySQL 与完整应用数据目录，尤其包含 master.key、加密配置、安装标记和头像。建议停服后取得对应时间点备份，保留原始文件权限；仅备份数据库不能恢复加密配置与头像。恢复后核对安装 ID 和主密钥指纹，不能混用另一系统的数据目录。

升级流程：停服 → 备份数据库与数据目录 → 用目标版本二进制显式执行迁移 → 启动并验证 → 恢复流量。不在普通启动时静默升级，不自动降级数据库。失败时按备份整体恢复，不只替换旧二进制。

实施验收至少覆盖：全新安装、并发安装、每个持久化步骤强制中断后恢复、数据库断连、密钥/标记缺失、安装 ID 不匹配、非 root/只读根容器、头像持久化、备份恢复、显式升级和创始人密码恢复。本次文档阶段没有执行这些运行验证。

安装锁和 DDL 行为参考 MySQL 官方文档：[命名锁](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)、[隐式提交](https://dev.mysql.com/doc/refman/8.4/en/implicit-commit.html)。容器例子的环境变量含义参考 [MySQL 官方容器文档](https://dev.mysql.com/doc/refman/8.4/en/docker-mysql-more-topics.html)。
