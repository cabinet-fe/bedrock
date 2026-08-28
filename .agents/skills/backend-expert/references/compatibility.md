# 兼容性堆砌

长期项目需求常变；默认一条通路，不堆双实现。

## 反例：新旧 API 永久双路径

```
func listUsers(params ListParams) {
    if params.UseLegacy {
        return listUsersV1(mapLegacy(params))
    }
    return listUsersV2(params)
}

func mapLegacy(params ListParams) V1Params {
    return V1Params{PageNo: params.Page, /* 大量字段映射 */ }
}
```

用户只要求接新接口时，应改调用方，而不是永久 `UseLegacy` / `/v1` + `/v2` 两套 handler。

## 正例

```
func listUsers(params ListParams) {
    return queryUsers(params)
}
```

同步改所有调用方；旧路由删除或留给 git 历史。

## 反例：为「少破坏」包一层适配永远不删

```
// deprecated: 转调新函数
func GetUserById(id string) User { return FindUser(id) }

func FindUser(id string) User { /* 新实现 */ }
```

无人要求保留旧名时，直接改名/改导入即可。

## 正例

只保留 `FindUser`；全局替换调用。

## 反例：无开关的 feature flag 空壳

```
const EnableNewAuth = true
if EnableNewAuth {
    requireAuth(ctx) // 唯一路径
}
```

## 正例

直接写鉴权逻辑，不要恒为 `true` 的旗标。

## 例外

用户明确说「要兼容旧客户端/旧字段/灰度」时，再写兼容，并尽量：

- 范围小、有删除条件或注释说明何时可删；
- 仍避免两套完整业务复制，优先数据适配一点、逻辑一份。
