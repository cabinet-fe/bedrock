# 用户管理（SysUser）API 文档

- **项目约定**: [API 约定](../_conventions.md)
- **是否需要认证**: 是
- **最后更新时间**: 2026-07-21

**响应信封**（只在此处写一次；下文各接口表格只描述 `data`）：

```json
{
  "code": 0,
  "msg": "success",
  "data": {}
}
```

## 目录

1. [新增用户](#1-新增用户)
2. [更新用户](#2-更新用户)
3. [删除用户](#3-删除用户)
4. [分页查询用户](#4-分页查询用户)

## 接口列表

### 1. 新增用户

创建系统用户并绑定角色；需要登录；权限 `sys_user:create`。

```
POST /admin/users
```

**请求体**:

| 字段名   | 类型     | 必填 | 说明                                    |
| -------- | -------- | ---- | --------------------------------------- |
| username | string   | 是   | 登录名，全局唯一                        |
| password | string   | 是   | 初始密码（明文传入，服务端加密存储）    |
| name     | string   | 是   | 显示姓名                                |
| phone    | string   | 否   | 手机号                                  |
| email    | string   | 否   | 邮箱                                    |
| orgCode  | string   | 是   | 所属组织代码                            |
| roleIds  | string[] | 否   | 角色 ID 列表                            |
| lockFlag | string   | 否   | 锁定标志：`0` 正常 / `9` 锁定，默认 `0` |

**响应体**（即信封里的 `data`）:

| 字段名   | 类型    | 说明     |
| -------- | ------- | -------- |
| `_(data)_` | boolean | 是否成功 |

**响应成功示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": true
}
```

### 2. 更新用户

按用户 ID 更新资料与角色绑定；需要登录；权限 `sys_user:update`。不传 `password` 则不改密。

```
PUT /admin/users
```

**请求体**:

| 字段名   | 类型     | 必填 | 说明                          |
| -------- | -------- | ---- | ----------------------------- |
| userId   | string   | 是   | 用户 ID                       |
| username | string   | 否   | 登录名                        |
| password | string   | 否   | 新密码；省略则不修改          |
| name     | string   | 否   | 显示姓名                      |
| phone    | string   | 否   | 手机号                        |
| email    | string   | 否   | 邮箱                          |
| orgCode  | string   | 否   | 所属组织代码                  |
| roleIds  | string[] | 否   | 角色 ID 列表（全量覆盖）      |
| lockFlag | string   | 否   | 锁定标志：`0` 正常 / `9` 锁定 |

**响应体**（即信封里的 `data`）:

| 字段名   | 类型    | 说明     |
| -------- | ------- | -------- |
| `_(data)_` | boolean | 是否成功 |

**响应成功示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": true
}
```

### 3. 删除用户

按用户 ID 列表批量逻辑删除；需要登录；权限 `sys_user:delete`。

```
DELETE /admin/users
```

**请求体**: 请求体为用户 ID 数组。

| 字段名   | 类型     | 必填 | 说明               |
| -------- | -------- | ---- | ------------------ |
| `_(body)_` | string[] | 是   | 待删除用户 ID 列表 |

**响应体**（即信封里的 `data`）:

| 字段名   | 类型    | 说明     |
| -------- | ------- | -------- |
| `_(data)_` | boolean | 是否成功 |

**响应成功示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": true
}
```

### 4. 分页查询用户

按条件分页查询用户列表；需要登录；权限 `sys_user:view`。分页字段约定见 [API 约定](../_conventions.md) · 分页。

```
POST /admin/users/page
```

**请求体**:

| 字段名     | 类型                   | 必填 | 说明                                      |
| ---------- | ---------------------- | ---- | ----------------------------------------- |
| current    | number                 | 是   | 当前页，从 1 起                           |
| size       | number                 | 是   | 每页条数                                  |
| moduleCode | string                 | 否   | 模块编码（鉴权常用）                      |
| condition  | Record<string, object> | 否   | 查询条件，key 为字段名；值见 ConditionVO  |
| sort       | Record<string, string> | 否   | 排序，key 为字段名，value 为 `asc`/`desc` |

常用 `condition` 字段：`username`（模糊）、`name`（模糊）、`orgCode`（精确）、`lockFlag`（精确）。

**响应体**（即信封里的 `data`）:

| 字段名  | 类型     | 说明           |
| ------- | -------- | -------------- |
| records | object[] | 当前页用户列表 |
| total   | number   | 总条数         |
| current | number   | 当前页         |
| size    | number   | 每页条数       |

`records[]` 单条：

| 字段名     | 类型     | 说明                              |
| ---------- | -------- | --------------------------------- |
| userId     | string   | 用户 ID                           |
| username   | string   | 登录名                            |
| name       | string   | 显示姓名                          |
| phone      | string   | 手机号                            |
| email      | string   | 邮箱                              |
| orgCode    | string   | 所属组织代码                      |
| lockFlag   | string   | 锁定标志：`0` 正常 / `9` 锁定     |
| roleIds    | string[] | 角色 ID 列表                      |
| createTime | string   | 创建时间（`yyyy-MM-dd HH:mm:ss`） |

**响应成功示例**:

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "records": [
      {
        "userId": "1888123456789012345",
        "username": "zhangsan",
        "name": "张三",
        "phone": "13800138000",
        "email": "zhangsan@example.com",
        "orgCode": "ORG001",
        "lockFlag": "0",
        "roleIds": ["1001", "1002"],
        "createTime": "2026-03-15 10:20:30"
      }
    ],
    "total": 1,
    "current": 1,
    "size": 10
  }
}
```
