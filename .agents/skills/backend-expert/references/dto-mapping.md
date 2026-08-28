# DTO：禁止逐字段搬运

字段一致时一次性对齐；创建/更新共用 body。`id` 走路径参数。

## 创建 vs 更新：绝大部分字段一致

创建和更新通常是**同一套 body 字段**。更新相对创建，常见差异只有：

- 多一个 `id`（偶尔还有 `version` 等极少数字段）；或
- **`id` 根本不进 body**：路径参数（如 `PUT /users/:id`），body 与创建相同。

因此禁止为「CreateDTO 一套 + UpdateDTO 一套」各写 50 行逐字段拷贝；差异用路径上的 `id` 或是否存在表达即可。

handler DTO、领域对象、存储模型三者字段名一致时，同样禁止逐字段搬运。

## 反例：逐字段赋值

```
// 定义 50 字段 + handler→领域 50 行 + 领域→存储又 50 行 → 爆炸
user.Name = req.Name
user.Age = req.Age
user.Email = req.Email
user.Phone = req.Phone
// ...
row.Name = user.Name
row.Age = user.Age
// ...
```

## 正例：批量对齐

```
user = req                          // 字段同名同形状：直接用 / 项目 mapper
row = UserRow{...user}              // 或项目已有的 copy / 构造函数
// 不要手写 dst.A = src.A 全表
```

## 反例：创建 / 更新各抄一套（仅因多了 id）

```
type CreateUserBody struct {
    Name  string
    Age   int
    Email string
    // ... 全字段
}

type UpdateUserBody struct {
    ID    string // 唯一差异
    Name  string
    Age   int
    Email string
    // ... 又全字段抄一遍
}

func create(body CreateUserBody) {
    save(User{Name: body.Name, Age: body.Age, Email: body.Email /* ... */})
}

func update(body UpdateUserBody) {
    save(User{ID: body.ID, Name: body.Name, Age: body.Age, Email: body.Email /* ... */})
}
```

## 正例：共用 body；id 在路径上

```
type UserBody struct {
    Name  string
    Age   int
    Email string
    // 创建、更新同一套；无 id
}

func create(body UserBody) { save(body) }

func update(id string, body UserBody) { save(id, body) }
// PUT /users/:id ，id 只替换路径，不进 body
```

## 反例：id 塞进 body 再逐字段拆出来

```
type UserBody struct {
    ID    string
    Name  string
    // ...
}

func update(body UserBody) {
    id := body.ID
    save(id, User{Name: body.Name /* 再抄全字段 */})
}
```

## 正例：id 常为独立参数，替换路径，不进 body

很多接口是 `PUT /resource/:id`，body 与创建一致——此时 **不要把 id 塞进 DTO 再逐字段拆出来**。

```
func handleUpdate(id string, body UserBody) {
    save(id, body) // body 与 handleCreate 相同
}
```

## 反例：擅自加完整鉴权空壳 / 通用映射器

用户只说「加一个更新用户接口」，却生成：

```
func mapCreateUser(req CreateUserRequest) CreateUserCommand { /* 逐字段 */ }
func mapUpdateUser(req UpdateUserRequest) UpdateUserCommand { /* 再抄一遍 */ }
func mapUserToRow(cmd any) UserRow { /* 再抄一遍 */ }
```

## 正例

- 三层字段一致 → 直接传，或一次项目已有的 mapper。
- 用户明确要求独立 DTO 再拆；仍禁止逐字段，优先批量。
- 路径上的 `id` 清掉即可，不必为 id 再写一套字段级拷贝。

## 例外（允许逐字段）

- 字段名不一致，需要显式映射（`user.Name = req.UserName`）。
- 仅少数字段要转换（时间戳、分转元）；其余仍批量，只对差异字段手写。
- 创建/更新字段集合真有多处不同（不只是 id）时，对差异字段分支，其余仍批量——禁止因此复制两整份 DTO。
