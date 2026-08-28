# 死代码与残留

重构与新功能都适用：引入过的 handler / 路由 / 中间件 / DTO，不用就删。

## 反例：重构后旧路径残留

```
// 已改用 ListUsers，旧 handler 仍留着
func GetUsers() { /* ... */ }

func ListUsers() { /* ... */ }
```

```
r.GET("/v1/users", GetUsers)  // 已无调用方
r.GET("/users", ListUsers)
```

## 正例

只保留 `ListUsers` 与 `/users`；删掉无引用 handler、路由注册、DTO、中间件。

## 反例：新功能顺手留下「备用」

```
const DEBUG_AUTH = false
func dumpToken() { /* 从未挂到路由 */ }

// 复制 handler 时带来的未使用请求体 / 响应 DTO
type LegacyUserDTO struct { /* 无引用 */ }
```

## 正例

不需要就不写；复制代码后立刻删未用 DTO、中间件、路由、import。

## Pass B 自检（做完再收工）

- [ ] 本次文件是否还有未引用的 handler / 函数 / DTO / import
- [ ] 路由表是否还有无 handler 或无调用方的条目
- [ ] 中间件是否已挂上且确有行为；空壳或恒 true 的 → 删
- [ ] 是否留下注释掉的大段旧实现 → 删，靠 git 回滚
