# AI Agents 工作规范

OctoDCP：Octopus-assisted Development & Collaboration Platform

## 目录结构

```txt
octodcp/
├── cmd/octodcp/        # 程序入口
├── docs/              # 需求、架构、API、数据库与部署设计
├── internal/          # 应用私有代码
├── web/               # 前端源码与构建产物
└── scripts/           # 辅助脚本
```

## 文档索引

文档变化时同步更新

- [docs/README.md](docs/README.md)：一期范围、已确认规则、工程实施约定及默认原型和设计文档入口。
- [docs/development.md](docs/development.md)：一期开发进度、安装至控制台闭环、视觉验收复盘、本地启动与数据库联调验证。
- [docs/ui-design.md](docs/ui-design.md)：柔和微增强视觉基线、中文排版、组件、导航、响应式、系统用词与交互规范。
- [docs/architecture.md](docs/architecture.md)：技术选型、模块边界、前后端结构与视觉基线、实施顺序。
- [docs/technology-decisions.md](docs/technology-decisions.md)：sqlx/sqlc、bcrypt/HMAC 与 MySQL 版本及命名的比较和建议。
- [docs/extensibility.md](docs/extensibility.md)：受控设置 KV、独立凭据表、仓储/事务接口和未来模块的扩展边界。
- [docs/domain-model.md](docs/domain-model.md)：注册与用户身份、创始人保护、权限、会话、头像及审计规则。
- [docs/api.md](docs/api.md)：一期 HTTP 接口、数据格式、授权、并发控制与错误契约。
- [docs/database.md](docs/database.md)：MySQL 表关系、字段职责、索引、事务与生命周期。
- [docs/schema.sql](docs/schema.sql)：七张一期业务表的 DDL 阅读副本；实际执行使用嵌入式 goose 迁移。
- [docs/deployment.md](docs/deployment.md)：容器目录、配置、主密钥、持久化安装锁、安装恢复、备份与升级设计。

交互原型仅供本地参考，不纳入 Git；正式前端开发以 `docs/ui-design.md` 和相关需求文档为基线。
