---
name: gin
description: Gin Web 框架实践。按 go.mod 里 github.com/gin-gonic/gin 的 1.y（优先 1.10 / 1.11 / 1.12）选用该版本 API。在编写 Gin 路由、handler、中间件、绑定与渲染时使用。
---

# Gin

Go 语法交给 `skills/langs/go`。ORM、日志库、鉴权方案跟项目走，不要在本技能里指定。用户没点名就不要顺手加完整鉴权中间件。

禁止凭训练数据写 API。先定 1.y，再读对应 reference。

## 定版本

1. 读 **`go.mod` 的 `require github.com/gin-gonic/gin`**（有 `replace` 以 replace 为准）。不要用本机 module cache 里碰巧较新的版本。
2. 取 **1.y**（`v1.12.0` → `1.12`），打开下表文件，按该文件写代码。

| 1.y | 文件 |
| --- | ---- |
| 1.10 | [references/1.10.md](references/1.10.md) |
| 1.11 | [references/1.11.md](references/1.11.md) |
| 1.12 | [references/1.12.md](references/1.12.md) |

未列出的更新 1.y：以已覆盖的最高档为底，再查官方 changelog / pkg.go.dev。更旧：不要使用本技能里的新 API。

## 路由与中间件

- 相关路径用 `Group`，公共中间件挂在 group 上，不要每条路由抄一遍。
- 中间件是洋葱：`c.Next()` 前是前置，后是后置。先注册的在最外层。
- `gin.Default()` = `Logger` + `Recovery`。生产要少中间件就 `gin.New()` 再显式 `Use`。
- 生产：`GIN_MODE=release` 或 `gin.SetMode(gin.ReleaseMode)`。
- 需要 405 时设 `engine.HandleMethodNotAllowed = true`（默认同路径错方法是 404）。
- 路径参数是 `:id`，用 `c.Param("id")`。不要 `{id}`。

## 绑定与响应

优先 `ShouldBind*`，自己写错误响应。`Bind*` 会直接 `AbortWithError(400)`，项目没有统一走这条就不要用。

```go
var req CreateUserReq
if err := c.ShouldBindJSON(&req); err != nil {
	c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	return
}
c.JSON(http.StatusCreated, user)
```

- 字段必须导出；用 `json` / `form` / `uri` / `header` + `binding` tag（go-playground/validator）。
- JSON body 用 `ShouldBindJSON`；query+form 混用 `ShouldBind`；路径参数 `ShouldBindUri`。
- 写完响应就 `return`。不要再 `c.JSON` 第二次。
- 结构体响应优先于满屏 `gin.H`（调试或真无固定形状除外）。
- `c.Error(err)` 留给已有的错误中间件；没有就在 handler 里直接写状态码。

## Context

`gin.Context` 来自 `sync.Pool`，请求结束后会复用。

- **禁止**把 `*gin.Context` 存到 struct / 全局 / 请求结束后的 goroutine。
- 要在 goroutine 里读请求快照：`cCopy := c.Copy()`，只用副本。
- 下游（DB、HTTP client）用 `c.Request.Context()`，不要把 gin Context 当 `context.Context` 往下传。
- 取值：已有 `GetString` / `GetInt` 等就用，不要 `c.MustGet` 再瞎断言。
- `ClientIP()` 依赖受信代理。默认信任所有代理不安全：没有反向代理就 `SetTrustedProxies(nil)`；有就填代理 CIDR，或 CDN 用 `TrustedPlatform`（`gin.PlatformCloudflare` 等）。
