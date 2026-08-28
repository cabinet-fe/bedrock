# 只转发的分层

结构跟项目已有分层走；一层只做一层的事。每层只 `return next()` → 删层。

## 反例：Controller → Service → Repo 全是转发

```
func (h Handler) Create(body UserBody) {
    return h.svc.Create(body)
}

func (s Service) Create(body UserBody) {
    return s.repo.Create(body)
}

func (r Repo) Create(body UserBody) {
    return r.db.InsertUser(body)
}
```

三层签名相同，中间两层没有规则、没有组合、没有事务边界——只是把参数往下传。

## 正例：停在真正干活的那一层

项目已是 handler 直打存储：

```
func (h Handler) Create(body UserBody) {
    return h.db.InsertUser(body)
}
```

项目已有 service 且里面有校验/事务/多存储：

```
func (h Handler) Create(body UserBody) {
    return h.svc.Create(body) // service 里确有业务
}
```

不要为新接口单独补一套空 service + 空 repo。

## 反例：为「整洁」再套一层

现有 `handlers/` + `store/` 已经够用，却新增：

```
handlers → usecase → repository → store
```

usecase / repository 文件里只有一行 `return next()`。

## 正例

新接口放进现有 `handlers/` 与 `store/`（或项目等价目录）。用户点名要新分层再加，且新层必须有非转发职责。

## 反例：接口 + 唯一实现 + 构造函数，只为可测

```
type UserStore interface{ Insert(UserBody) }

type userStore struct{ db DB }

func (s userStore) Insert(body UserBody) { return s.db.InsertUser(body) }

func NewUserStore(db DB) UserStore { return userStore{db} }
```

全仓没有第二个实现，测试也只打这一条路径——接口是噪音。

## 正例

直接依赖项目已有的 `DB` / store 具体类型。需要 fake 时再抽，且有第二处真实实现或测试替身才算数。

## 自检

- 去掉某一层后行为是否仍成立？成立则删。
- 这一层是否只有参数原样传递、字段原样拷贝？是则删，或并入邻层。
- 新目录是否只为对齐某篇架构文章、与仓库其余模块不一致？是则不要建。
