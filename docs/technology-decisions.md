# 数据访问与密码方案比较

更新日期：2026-10-18。本阶段采用 sqlx + MySQL 驱动 + goose，密码使用 bcrypt；最少 8 个字符已确认。这些 Go 依赖已安装，安装服务已使用 bcrypt 创建创始人凭据；登录、改密及会话管理已接入。产品确认与工程实施约定见 [设计入口](README.md)，验证状态见 [开发说明](development.md)。

## 1. 数据访问：采用 sqlx

选型时仓库只有 Go + Gin 骨架，sqlc 尚未投入运行，因此选择 sqlx 不涉及已有查询代码的迁移。目前 MySQL 安装与账号管理适配器已使用 sqlx，继续保持接口与驱动隔离。

| 比较项 | sqlx | sqlc |
| --- | --- | --- |
| 工作方式 | 在 `database/sql` 上扩展扫描、命名参数和事务句柄 | 读取 schema 和 SQL，生成 Go 类型及查询方法 |
| 编辑与构建 | 编写 SQL 和带 db 标签的行结构即可，不新增生成步骤 | 修改 schema/SQL 后运行生成器，提交并检查生成结果 |
| 字段与参数检查 | 字段映射、查询错误主要在运行及集成测试发现 | 能把其解析范围内的 SQL/类型问题前移到生成阶段；不代替实库验证 |
| 动态列表筛选 | 按白名单拼 SQL 片段较直接；值仍全部绑定 | 适合显式固定查询；组合较多时需要额外设计参数或多份查询 |
| 事务与锁 | 明确共享同一 `sqlx.Tx`，可直接写 MySQL 锁定语句 | 明确绑定同一事务；同样需要服务层设计锁顺序 |
| 维护代价 | 手工维护行结构和扫描契约，必须覆盖关键查询 | 增加生成器版本及 schema/查询生成契约的维护 |
| 更适合的倾向 | 小步实现、减少工具链、接受 SQL 集成测试 | 重视生成类型约束、稳定固定查询较多、接受代码生成 |

以上属于工程取舍，不是性能基准。对 OctoDCP 一期，我建议 **sqlx + go-sql-driver/mysql + goose**：保留手写 SQL 和明确事务，减少一个生成工具；用户管理、审计等筛选也容易按需扩展。若团队更看重生成期的类型约束，保留 sqlc 也是成立的选择，不能仅以“需要生成”认定它复杂或不可扩展。

采用 sqlx 时必须一起落实：

- SQL 和私有行结构集中在 `internal/store/mysql/`，不进入 Handler；HTTP DTO、领域对象和数据库映射仍分开。
- 明确列名及别名，不用 `SELECT *`；多表同名列必须显式 alias。保留默认严格映射，不通过 `Unsafe()` 忽略列问题。
- 使用 Context 方法与超时；查询结果有分页或上限，不让 `SelectContext` 无界加载全表。
- 所有值参数绑定，动态条件、表名与排序只来自服务端定义。没有自动 CRUD、自动迁移或通用数据库权限框架。
- 账号、凭据、会话与审计共享同一事务；服务通过仓储/UnitOfWork 接口协作，sqlx 句柄只留在 MySQL 适配器。关键验证包括创始人保护、管理员并发互管、版本冲突及软删除筛选。
- MySQL 集成检查至少覆盖 8.0 与 8.4 系列，验证 SQL、字段映射、外键/CHECK、事务与迁移；纯 mock 无法证明跨版本兼容。

依据：[sqlx 官方指南](https://jmoiron.github.io/sqlx/) 与 [sqlc MySQL 教程](https://docs.sqlc.dev/en/latest/tutorials/getting-started-mysql.html)。上述项目建议是基于这些能力作出的判断，并非工具官方对本项目的推荐。

## 2. 密码：可用 bcrypt 简化，不单独使用 HMAC

| 方案 | 需要维护什么 | 主要取舍 |
| --- | --- | --- |
| Argon2id | 内存、时间、并行度、编码和资源上限 | OWASP 对新系统的优先建议；没有 bcrypt 的 72 字节限制 |
| bcrypt | cost；库负责随机盐与编码 | Go 调用较直接，适合本次简化偏好；有 72 字节限制，不具备 Argon2id 的内存硬化特性 |
| 单独 HMAC | 私钥、盐和自定义验证格式，但仍缺少慢速口令派生 | 是消息认证工具，不能作为 bcrypt/Argon2id 的等价替代；密钥泄露后可快速猜测密码，轮换与恢复还会耦合主密钥 |
| PBKDF2-HMAC | 随机盐、足够迭代次数、派生长度及格式 | 是完整的密码派生算法，与单次 HMAC 不同；本次不作为简化首选 |

采用 bcrypt，实施约定为：

1. 用 `golang.org/x/crypto/bcrypt` 的 `GenerateFromPassword` 和 `CompareHashAndPassword`。完整字符串保存在 `user_password.password_hash`，无需单独 salt 表、算法列或全局 pepper。
2. cost 初值建议 12；在目标容器测量后决定，最低 10。限制并发哈希和在线猜测；比较函数本身不负责登录限流。
3. 新密码至少 8 个 Unicode 码点、最多 72 个码点且最多 **72 UTF-8 字节**。常见汉字通常每个 3 字节，约 24 个已到上限；ASCII 通常可到 72 个。界面同时给出字节提示，后端最终校验。
4. 超长输入显式拒绝，不截断，也不为绕过限制自行叠加 SHA/HMAC 预哈希。若未来需要超过 72 字节的中文口令，再通过版本化方案迁移到 Argon2id。
5. 哈希自带算法前缀和 cost，字段仍保留 255 字符以便未来换算法。成功登录时可检查 cost 并升级，必须避免旧哈希覆盖并发重置后的新密码。
6. 系统主密钥继续用于加密配置，与用户密码哈希分离。用户密码不会因为主密钥轮换而需要整体重算。

这是接受 bcrypt 限制的简化取舍，不将它描述成比 Argon2id 更安全；最少 8 个字符是本项目确认的产品规则，不代表 OWASP 对无 MFA 密码长度的推荐。具体校验同步于 [领域](domain-model.md)、[API](api.md) 和 [数据库](database.md)，默认静态原型已同步长度与字节校验，正式哈希仍待实现。

依据：[Go bcrypt 文档](https://pkg.go.dev/golang.org/x/crypto/bcrypt) 明确 72 字节限制及标准调用；[OWASP 密码存储指南](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html) 说明算法优先级、bcrypt cost 下限及预哈希注意点。

## 3. MySQL 8+ 与命名

业务表名统一为单数 snake_case，DDL 用反引号引用表名；表名不决定 HTTP 集合资源的命名。`goose_db_version` 属于迁移工具维护对象，不由应用自行重命名。

当前最低版本建议明确为 **8.0.16**：更早的 MySQL 8.0 对 CHECK 的支持不足，不能宣称它们会执行本稿的约束。8.0 和 8.4 使用同一套迁移及查询，8.4 镜像只作容器示例；更高主版本通过兼容验证后加入支持范围。依据：[MySQL CHECK 约束版本说明](https://dev.mysql.com/doc/refman/8.0/en/create-table-check-constraints.html)。
