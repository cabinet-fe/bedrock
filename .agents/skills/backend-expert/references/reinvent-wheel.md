# 造轮子与乱加依赖

先复用项目能力，再考虑新库。

## 反例：已有 HTTP client 仍手写一轮

项目已有 `pkg/httpx` / 内部 SDK：

```
func fetchOrder(id string) Order {
    req := newRequest("GET", baseURL+"/orders/"+id)
    resp := defaultClient.Do(req)
    return decode(resp)
}
```

## 正例

```
func fetchOrder(id string) Order {
    return httpx.Get[Order]("/orders/" + id) // 项目已有 client
}
```

## 反例：已有 JWT / 鉴权中间件仍新造

```
func parseToken(r Request) {
    raw := r.Header("Authorization")
    // 手拆 Bearer、手验签、手读 claims
}
```

项目已有 `middleware.Auth` 或现成 JWT 封装时，禁止再造一份。

## 正例

```
r.Use(middleware.Auth) // 项目已有
```

## 反例：已有分页仍新造

```
type MyPage struct {
    Offset int
    Limit  int
}
```

已有 `PageQuery` / `pkg/paging` 时，禁止再造。

## 正例

```
list(params PageQuery) // 项目已有
```

## 反例：忽略已安装的 Agent Skill

需要按团队 HTTP 框架或 ORM 写 handler，却凭训练记忆自创生命周期与文件布局；项目已安装对应库的 Skill 时，应先读 Skill 再写。不要在本技能里挑选 Gin vs Echo、Axum vs Actix——跟项目已用的走。

## 正例流程

1. 搜 `internal/` `pkg/` 已有 client、中间件、分页、鉴权  
2. 查 `go.mod` / `package.json` / `Cargo.toml` 是否已有对应依赖  
3. 查已安装 Skill 是否覆盖该库或语言版本  
4. 仍不够 → 用现有依赖的 API；若要新依赖，先问用户  

## 允许新建封装的条件

- 搜过确认不存在；
- 逻辑在本处已重复或即将多处使用；
- 不引入新依赖，或用户已批准新依赖。
