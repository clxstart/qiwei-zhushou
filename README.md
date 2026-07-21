# 企微助手（qiwei-zhushou）

企业微信交付助手 V1：建群自动交付说明 + 群账务闭环 + 后台总账。

规格：[`SPEC-V1-企微交付助手.md`](./SPEC-V1-企微交付助手.md)  
Tickets：[`.scratch/qiwei-delivery-v1/`](./.scratch/qiwei-delivery-v1/)

## 工程形态

- **单 Go 进程**：HTTP API + 企微回调（后续）+ 静态管理壳
- **MySQL** 持久化
- **分层**：`cmd` 编排 → `app` 装配 → `httpapi` / `auth` / `store` / `config`

## 快速开始

### 1. 准备配置

```bash
cp configs/app.example.toml configs/app.toml
# 编辑 mysql.dsn / admin / session.secret
```

### 2. 准备 MySQL

创建库（示例）：

```sql
CREATE DATABASE qiwei_zhushou DEFAULT CHARACTER SET utf8mb4;
```

### 3. 运行

```bash
go run ./cmd/server -config configs/app.toml
```

打开 http://localhost:8080 ，使用配置中的管理员账号登录。

### API（票 01）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/health` | 探活（含 DB） |
| POST | `/api/v1/admin/login` | `{"username","password"}` |
| POST | `/api/v1/admin/logout` | 登出 |
| GET | `/api/v1/admin/me` | 当前管理员（需会话） |
| GET | `/api/v1/admin/techs` | 技术花名册列表 |
| POST | `/api/v1/admin/techs` | 创建技术档案 |
| GET | `/api/v1/admin/techs/{id}` | 查看技术档案 |
| PUT | `/api/v1/admin/techs/{id}` | 更新技术档案 |
| DELETE | `/api/v1/admin/techs/{id}` | 删除技术档案 |

### 测试

```bash
go test ./...
```

## 目录

```text
cmd/server/          入口
internal/app/        依赖装配
internal/config/     配置加载
internal/auth/       管理员鉴权与会话
internal/store/      MySQL 与迁移
internal/httpapi/    HTTP 路由
web/dist/            管理壳静态资源
configs/             TOML 配置
docs tickets         .scratch/qiwei-delivery-v1/
```
