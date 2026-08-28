# 过度设计与防御式编程

简单需求用直接写法；禁止预支抽象。

## 反例：一个实现还抽 Repository 接口

```
// 需求：按 id 读用户，全仓只有这一种存储
type UserRepository interface {
    Find(id string) User
}

type userRepo struct{ db DB }

func (r userRepo) Find(id string) User {
    return r.db.GetUser(id)
}

func NewUserRepository(db DB) UserRepository {
    return userRepo{db}
}
```

## 正例

```
func findUser(db DB, id string) User {
    return db.GetUser(id)
}
```

项目已有仓储层且每个模块都这么写 → 跟项目走，不要为「接口更干净」再套一层只有一个实现的接口。

## 反例：UseCase 只调一行

```
type CreateUser struct{ repo UserRepository }

func (uc CreateUser) Execute(body UserBody) {
    return uc.repo.Create(body) // 唯一行为
}

func (h Handler) Create(body UserBody) {
    return h.uc.Execute(body)
}
```

handler 到存储之间没有规则、没有组合——这一层是空的。

## 正例

```
func (h Handler) Create(body UserBody) {
    return h.db.CreateUser(body)
}
```

有真实业务（校验组合、多存储、发事件）再留 service / use case。

## 反例：空 DDD 目录

```
internal/domain/user/entity.go      // type User struct { ... }
internal/app/user/create.go         // 一行转调
internal/infra/user/repo.go         // 一行转调
internal/interfaces/http/user.go    // 一行转调
```

四个包只为「像整洁架构」。用户没点名 DDD，就不要铺这套目录。

## 正例

跟项目现有切分：已有 `handlers/` + `store/` 就写两层；已有三层就三层。不要为新接口单独发明第四层。

## 反例：过度防御

```
func priceOf(order *Order) int {
    if order == nil { return 0 }
    if order.Items == nil { return 0 }
    if len(order.Items) == 0 { return 0 }
    if order.Items[0].Price < 0 { return 0 }
    return order.Items[0].Price
}
```

调用方与校验已保证订单有行项目时，不必层层守门。

## 正例

```
func priceOf(order Order) int {
    return order.Items[0].Price
}
```

边界在入口校验一次；内部按已成立的前提写。

## 反例：预支分页框架 / outbox / flag

```
// 只有一个列表接口，却做成通用分页框架 + outbox + feature flag
type Paginator[T any] struct{ /* ... */ }
type OutboxPublisher struct{ /* 从未发消息 */ }
const EnableNewList = true
```

## 正例

列表按项目已有查询方式写（已有分页工具就用，没有就本次用到的 `limit/offset` 或 cursor）。消息、flag 用户没点名就不加。

## 抽公共的时机

- 已出现 ≥3 处真实重复，或用户明确要求复用 → 再抽。
- 「以后可能有第二个仓储实现」→ 不抽。
