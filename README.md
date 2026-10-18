# OctoDCP

[![Test](https://github.com/ravenmk2/octodcp/actions/workflows/test.yml/badge.svg)](https://github.com/ravenmk2/octodcp/actions/workflows/test.yml) [![Release](https://img.shields.io/github/v/release/ravenmk2/octodcp)](https://github.com/ravenmk2/octodcp/releases) [![License](https://img.shields.io/github/license/ravenmk2/octodcp)](LICENSE)

Octopus-assisted Development & Collaboration Platform

一期设计基线见 [docs/README.md](docs/README.md)，包含已确认范围、架构、领域与权限、API、数据库、容器部署及 [UI 设计规范](docs/ui-design.md)。

一期开发进行中。已实现安装、创始人自动登录、账号与会话、控制台用户管理及系统设置，正式界面沿用已确认的柔和微增强原型；头像和完整审计查询继续迭代。进度、启动与验证方法见 [开发说明](docs/development.md)。

```sh
bash scripts/build-web.sh
go run ./cmd/octodcp serve
```

前端构建需要 Node.js 22.12+ 和 npm，安装页面位于 `/setup`。交互原型不参与正式构建或 Git 提交。数据库由部署者准备专用空库，程序安装时执行迁移，不手动导入 `docs/schema.sql`。
