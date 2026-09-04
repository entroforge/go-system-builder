# Design Grammar

> 编译自：DESIGN.md@vX.Y.Z
> 不变量 / 受控变量 / 选择规则 / 例外必须齐全。数值表不是 Grammar。

每条规则使用：

```text
因为【Kernel/Law】
所以在【场景/任务】
优先采用【设计关系】
避免【冲突关系】
允许【受控变化】
通过【Proof】判断是否成立
```

九个维度各至少一条，或显式声明“本产品该维暂弱”并写入 `DESIGN.md` 设计债务。

## LAW-01 {名称}

信念：
依据：{Evidence Field 条目}
设计后果：
适用条件：
代价：
Do：
Don't：
证明：{Proof 路径}

### 编译

因为【LAW-01】所以在【场景】优先【关系】避免【冲突】允许【受控变化】通过【Proof】判断。

| 维度 | 消费者端 | 运营端 |
|:--|:--|:--|
| Information | | |
| Composition | | |
| Color | | |
| Typography | | |
| Shape & Surface | | |
| Image & Icon | | |
| Interaction | | |
| Content | | |
| Motion | | |

重复本结构覆盖 LAW-02 … LAW-0n。

## Invariants

- {所有 Surface 必须保持的设计关系，例如强调稀缺、状态可解释}

## Variants

- {可随 Surface、任务或密度变化的部分，例如留白、字号、导航、表格密度}

## Selection Rules

| 条件 | 选用模式 | 不要选用 |
|:--|:--|:--|
| 探索 | | |
| 判断 | | |
| 承诺 | | |
| 等待 | | |
| 恢复 | | |

## Semantic roles

P2 起与 Token 对齐。P0/P1 先冻结角色名称，不冻结色值。

| 角色 | 含义 | Grammar 回指 | Token（P2） |
|:--|:--|:--|:--|
| action.promise | 用户将做出可追踪承诺 | LAW-01 | |
| evidence.support | 支撑判断的依据 | LAW-01 | |
| status.blocking | 阻止继续的错误或权限 | LAW-0n | |
