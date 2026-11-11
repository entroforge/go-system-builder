# L4 检查执行、环境与旧执行恢复

## 1. 机制为什么存在，适用于哪里

“运行一次测试”同时涉及命令、被测内容、依赖、环境、执行位置和结果接收。工具存在不代表环境可用，进程退出也不直接解释产品是否正确；若这些事实没有分开，执行者容易反复重试环境故障，或把一份旧日志当成新输入已经通过。

本机制让真实检查有明确配置、可解释失败和可恢复记录。它也保留已登记旧 execution 的固定观察、安装与搬迁能力，以及在原人意范围内减少重复批准的有限修复授权。各项能力都有原消费者，不组成另一条子代理执行生命周期。

新 S5～S9 子代理由 Claude Code 原生创建和运行，Start／Stop 衔接工程输入与结果。本文的旧 workspace prepare／launch、bootstrap、session 与 lease 只服务原冻结记录；不能变成新任务的必需前置。原生派发见[子代理工程衔接](L4-agent-dispatch-governance.md)，Git 集成与回收见[Worktree 机制](L4-worktree-governance.md)。

本篇承接 [L1](../l1-principles/L1-design-principles.md) 的分工、成本、必要约束和独立背书：机器记录它实际观测的事实，Agent 判断检查说明什么，人决定原有价值与授权问题。增加 runner 或摘要字段不能代替专业验证。

## 2. 四层事实怎样连接

| 层次 | 要回答的问题 | 原消费者 |
| --- | --- | --- |
| 检查要求与配置 | 谁需要检查什么，在哪里、用什么命令与输入 | 原 TASK／Assignment／修复责任和 CheckProfile |
| 实际工具执行 | 命令是否运行，实际树、环境、退出、日志是什么 | 原 runner 及其工具回执 |
| 工具事实接收 | 这份回执是否来自本次原调用，仍属于当前责任 | 原 Writer 或 Main 检查点 |
| 专业结论与阶段完成 | 观察是否支持断言、覆盖是否充分、能否进入下一步 | 实际角色、领域消费者、阶段门及人类入口 |

有一份日志或自洽 JSON，只能说明某个文件存在。原工具事实还要通过真实调用和原接收关联；即使接收了一个失败回执，也只是诚实记录失败，不表示本人已经完成或阶段通过。

Main 必需集成检查、Worker 自检和独立验证分别承担责任。Worker 树检查不能认证合并后的 Main；Main 命令成功也不替代 Reviewer 的覆盖与风险判断。详细阶段方法见 [S6](../l3-stages/L3-S6-build.md)、[S7](../l3-stages/L3-S7-verification-round.md)和 [S9](../l3-stages/L3-S9-repair.md)。

## 3. CheckProfile 怎样描述可执行检查

### 3.1 引用原检查，不维护第二清单

CheckProfile 是原检查的显式执行配置。它按 `check_index` 与 `command` 对应已有 `required_checks` 中的准确位置；不能用同名 profile 替换命令、把 `locked:` 元数据当 shell，或另维护一份漂移的检查列表。

| 字段组 | 作用与设计理由 |
| --- | --- |
| id、version、check_index、command | 固定本项配置与原命令槽，供接收和重放核对 |
| location、timeout_seconds | 明确 Main／Worker 位置及有界执行时间 |
| environment_policy、environment | 区分继承环境与仅声明环境，声明值及未设置状态有不同含义 |
| dependencies、fixtures | 指定实际依赖和测试材料的仓库路径与用途 |
| input_closure、side_effects | 说明掌握了什么输入，是否可能生成或改变内容 |
| reuse_policy、reuse_mode | 表达允许的有限复用与观察／启用选择，不自动授予复用资格 |

当前 profile 的超时范围为 1～300 秒，环境名称有明确白名单。依赖／fixture 使用实际仓库相对路径，不能指向控制文件或经符号链接逃出来源根；原冻结步骤把必要来源纳入既有输入关联。

已有执行的 profile、runner 位置和输入发生实际变化时，由原责任按替换／修订能力处理，保留旧接收和专业工作。Hook 及其可达消费者不计算、登记或校验来源、输入、回执、权限或日志的散列，不将此工作转交作者。

### 3.2 一个静态检查配置的形状

下例对应原检查槽 0 的命令 `test -f src/example.go`。它仅说明 profile 的合法结构，不证明该文件存在、内容正确或本项目已经执行过检查。

```json
{
  "id": "check-example-file",
  "version": "v1",
  "check_index": 0,
  "command": "test -f src/example.go",
  "location": "worker",
  "timeout_seconds": 30,
  "environment_policy": "declared_only",
  "environment": [],
  "dependencies": [],
  "fixtures": [],
  "input_closure": "whole_tree",
  "side_effects": "none",
  "reuse_policy": "same_input_same_responsibility",
  "reuse_mode": "shadow"
}
```

这里 shadow 仍执行实际检查，只观察是否具备复用条件。文件存在检查只回答一个机械问题，不能替代行为测试、专业结论或独立验证。

## 4. 检查前先弄清环境能力

### 4.1 工具入口与完整就绪分别表达

Main 入口预检可以检查 shell 及简单字面命令的可执行入口。找到 `go`、`node` 或 shell，只能证明该入口存在；不能证明依赖、服务、配置、fixture、测试数据或网络条件已经齐全。

复杂 shell、展开或控制语法没有静态工具闭包时，当前能力报告保留 unverified，而不是执行命令探路或据此默认 ready。显式 profile 可检查它声明的环境和文件来源，仍不意味着任意脚本的全部外部输入已被证明。

环境选择应在原责任准备时明确。选择 Main 也需要实际准备，不是天然可用；选择受限 Worker 后，隔离不可用不能自动切 Main 或换成无限制执行。

### 4.2 Worker 的实际预检

原受限 Worker runner 会检查输入、profile、必要文件与实际隔离路径。存在可执行检查时，还要验证可复制的源树和隔离能力；当前副本大小有 512 MiB 上限，特殊文件等不受支持输入需明确反馈。

检查工具已安装，不等于 user namespace、UID 映射或 seccomp 在宿主可用。预检和真实尝试应留下可解释诊断，由环境 owner 处理；不能把它当成产品断言失败，也不能默默放宽边界。

网络依赖需要预装的离线材料，或在原合同允许时明确选择 Main 检查。已经 ready 的旧执行不能临时换 runner。不同操作系统的能力需分别验证，不能从 Claude Code 自身的 sandbox 推导本 runner 已支持它。

## 5. 实际检查在哪里运行

### 5.1 新原生工作与旧受限 runner 分开

新原生 Builder 按真实 TASK／修复要求在本人实际工作区自检，并如实记录观察。普通工具不向框架申请逐次检查许可，也不需要先建立旧 execution、bootstrap 或 activation。

本文的受限 Worker runner 是已有登记检查能力：只选择原冻结命令的索引，验证原输入后，在临时副本执行，生成工具事实供原消费者接收。它不替代 Claude Code 的正常执行机制。

部分旧辅助入口虽名为 native owned check，仍要求原已接受 PLAN、固定上下文及 activation 链。函数名不证明适用于新的 `s6-native-v1`；不能把这些前提重新加给新原生 Worker。原角色指引必须以当前责任合同为准。

### 5.2 Main 必需检查与 Worker-owned 检查分别配置

S6 canonical Record 的 Main `required_checks` 和本人 `WorkerChecks` 是不同集合。profile 应对应各自原索引，不能把 Main 命令复制到 Worker 当作完成凭据，也不能因 Worker runner 集合为空就免除实际 TASK 自检。

Main 在合并后实际树运行必要检查，保留原 checkpoint。S9 的真实前检描述已经发生的观察，Main 另按修复目标与变化选择必要集成命令，不由前检自动冻结未来检查。

原兼容双位置检查与显式 post_merge 的取舍、实际目标 HEAD 及重验要求，由 Worktree 篇说明。本篇不以节省一次执行为理由省略原 Main 检查。

### 5.3 临时副本隔离什么

当前 Linux runner 使用 Bubblewrap 与 seccomp：Main、原 Worker 及 Git 元数据按只读来源处理，把临时副本映射为检查 cwd，生成文件留在副本；临时目录也单独安排。完成后丢弃副本，不把测试生成物自动变成产品交付。

实际网络策略保留宿主 network namespace，并以原 seccomp 规则拒绝 socket／相关调用；不能描述成已经建立了独立网络命名空间。它也保留宿主文件系统的只读可见面，因此框架不能声称掌握任意 shell 的全部输入。

命令、位置、平台与隔离失败都应如实表达。不支持的宿主直接给出能力限制，不能悄悄运行一个无隔离 Worker 检查。原生平台隔离、受限检查副本与工程写范围，各有实际用途，不能相互冒充。

## 6. 工具回执怎样成为原接收事实

### 6.1 runner 记录实际发生的内容

实际执行保留原命令、责任关联、被测副本、环境／runner／依赖／fixture 观察、耗时、状态及原日志。新回执不计算或登记完整性、日志及输入散列；旧回执按原路径读取，历史散列不重算、校验或变成复用资格。

runner 记录实际环境及执行结果。已知输入发生影响检查的变化、取消、超时、日志写入失败或结果发布失败时，如实保留实际状态，不制造可复用 PASS；不能声称系统凭路径自动发现所有输入变化。失败保留诊断价值。

### 6.2 真实调用捕获与领域接收分别成立

原 runner 通过原调用交出实际命令、日志、执行结果及责任关联；原 CLI 沿 Writer 保存工具事实，Main runner 沿原集成 checkpoint 保留事实。调用链不采集或登记字节散列、Git OID 或其他内容指纹。

作者不手写接收元数据，单独放置日志或自造文件也不能冒充框架实际执行。记录只说明原调用可知的事实，不声称具有独立字节完整性认证或自动发现所有改动的能力。

原工具接收引用沿实际调用、原 profile 和责任关联；单纯日志存在不够，不计算散列验证日志或回执。当前普通执行回执引用有有界保留，过期只意味着不能复用，不能抹去失败；不同 owned-check 集合按其原索引与保留规则处理，不能把一处数量限制推广为所有回执统一配额。

### 6.3 接收失败不补造历史

文件已发布而 Writer 未接受时，可能只有一次真实尝试而没有原接收引用。恢复应按该入口支持的原事实核对或重跑；不能从日志文件补写一个“已接受”记录，也不能将运行中诊断当成回执。

这类事实只证明工具执行，不授予写权限、批准合同或使责任 done。完整专业结论仍交原领域消费者判断。

## 7. 有限复用为什么必须很窄

### 7.1 重复输入不等于重复命令文字

同一个命令可以读不同环境、宿主文件、工具版本或外部服务。相同命令或路径不能证明当前输入相同；Hook 不计算 whole_tree 散列或登记 Git OID 来判断。

复用须有原已接收 PASS，且原责任和实际配置适用；Main／原执行责任按既有工作处理已知影响检查的输入、环境和工作变化。无散列后，不能独立认证输入未发生过未记录变化；这种未知本身不等于已知不适用，也不要求作者另作判断、声明或证明。已知输入或环境变化确实影响本次检查，或者原记录不属于实际责任／配置时，不把原 PASS 冒充当前检查，按原责任执行所需检查；不计算或重验散列、Git OID 或整树／逐文件内容来补证。

权限观察来自原 runner 预检，包含隔离工具、shell／运行库、filter／策略、内核与身份、控制根／仓库／Worker／cwd 和原授权来源的实际可得事实；已知会影响检查的变化按原责任处理。历史观察不完整本身不是补交平台证明或认证全量运行环境的理由；需要新执行时仍沿原 runner 如实处理实际隔离能力和失败，不将未执行冒充成功。

### 7.2 默认观察，显式启用仍保留原责任

当前 `reuse_policy` 不允许或 profile 不存在时正常重跑。shadow 可以反馈 would_reuse，但仍运行；显式 enabled 只允许原 Worker 的 `same_input_same_responsibility` 策略，且原 profile 为 declared_only、无副作用、whole_tree 并使用当前已闭合静态语法。这里 whole_tree 是已有 profile 声明，不要求计算树摘要或比较全部文件；同输入不再被表述为机器已证明，实际未知及已知变化按 §7.1 处理，不因这一放宽扩展 profile 的适用命令或责任范围。

当前静态语法范围仅 `true`、`false`、`test -f`、`test -s` 的受测树内字面路径。它不覆盖任意测试工具、流水线或脚本。即使命令属于此集合，旧失败、原记录真实不可读或未被原消费者接收，也不能作为已收 PASS 命中；历史权限观察缺项不凭空推断失败，也不要求补一份证明。

真正命中返回原已接收引用，不新造一份 PASS 或刷新执行时间。旧 shadow 回执不因为配置改 enabled 就升级资格；冻结配置变化仍走原机制。

### 7.3 必须重新执行的责任优先

Main 必需集成检查、独立验证、新的判别假设及 fresh 要求均优先重新执行。当前检查协调器也为修复状态带 fresh guard，不能把 S9 专业复验变成同名命令缓存命中。

已知换责任、输入或环境会影响检查时重新执行；搬迁路径只保留可读历史，不将旧检查冒充新的实际执行。

## 8. 并发检查与运行诊断怎样配合

相同原责任、profile 和源树的进程锁覆盖执行及原 Writer 接收，避免并发请求各跑一次后互相覆盖。竞争请求得到有界 run 引用；取得锁后还要重读当前 owner、execution、输入与适用 S9 原责任，不能把排队前快照当作永久许可。

`run.json` 的 waiting／running／finished／interrupted 是检查尝试诊断，独立于工程五态和专业结论。finished 可以带失败，PID 也不是可用的完成证明。进程死亡释放锁后，原 unfinished 记录保留为 interrupted，再核原回执或重跑。

`check-running` 只读有界诊断，不轮询或恢复 Claude 子代理，不自动将 Worker 改 active。Main 原集成锁仍独立承担其 Git 事务，不能让一次检查锁代替整个工作区串行化。

## 9. 失败怎样解释和继续

| 实际失败 | 要说明什么 | 恢复责任 |
| --- | --- | --- |
| capability_missing | 哪个入口、runner 或声明材料不可用 | 原环境 owner 修复，不静默换 ready runner |
| process_exit | 命令实际退出与日志，尚不能判产品断言 | 专业作者结合输出与检查合同解释 |
| timeout／cancelled | 本次中断、实际候选和原诊断 | 保留现场，按原入口及有效输入重试 |
| diagnostic_io | 日志／回执存储或发布失败 | 环境／系统责任恢复存储，不丢候选 |
| input_drift | 实际被测内容或环境在过程中改变 | 复核输入，重新形成有效检查 |
| unclassified_failure | 当前未能准确归因的错误 | 先看原错误和日志，再决定处理 |

当前失败 DTO 可带 code、category、owner、check、observed、repair、retry_condition 及回执／日志引用。分类不是根因诊断：例如 runner 进程因宿主配置退出，可能只得到 process_exit，仍需看日志，不应机械生成产品 Finding。

环境恢复后重试同一候选，与需要修改代码形成新交付是不同动作。失败检查保留在原证据及集成工序中，Main 不自动 reset、stash、确认或清理来掩盖失败。具体集成恢复见 Worktree 篇。

## 10. 原冻结 execution 怎样继续使用

### 10.1 原输入与执行身份保持固定

已有 execution 保留 owner、generation、范围、base、原输入和 profile；bootstrap receipt 固定实际安装文件，原平台 session 与 launch 记录只定位该旧执行。exit 0 不代表任务完成，lease 也不代表工程批准。

当前操作按[原生工作区指南](../../docs/workspace-integration.md)展开；已登记执行的具体命令与 runner 参数集中在[冻结执行恢复参考](../../docs/workspace-execution-recovery.md)。读取恢复命令前先确认原记录采用的协议，不凭某个旧章节或命令存在就要求新任务增加步骤。

旧执行的准备、固定 session、launch lease 和恢复不得转用于新原生任务。S9 `native_delivery` 只在返回后记录实际 Git 来源，供原 Result／Main 集成消费，不具备这些旧启动能力。

### 10.2 安装中断只恢复可证明的原内容

旧新建工作区可能继承已提交 launcher／settings。原 preparing、尚未发布 bootstrap receipt 的窗口仍须保护现场；若根据已有实际内容不能安全判断能否替换，则保留文件交系统／Main 处理，不凭旧 Git OID、散列或缺失证明猜测覆盖。

来源不可读、已有用户内容或结果不确定时保持原现场；重试不覆盖用户后来修改。

Main 权限、Runtime、journal 和凭据不会因为恢复安装而复制进 Worker。新原生责任也不为使用某个检查器安装一套 bootstrap。

### 10.3 作者位置按冻结上下文读取

当前原生题面提供准确 authoring path；删除 `.claude/submissions/` 等旧式执行路径及其兼容恢复接线，不按目录中哪个文件存在猜版本。用户实际文件保留，不因清理代码删除。

某条平台写限制不能靠让作者换 Bash、修改全局宿主权限或复制一份控制文件绕过。准确私人路径的用途是正常保存原专业内容，不授予新的产品范围或另建接收入口。

## 11. 固定观察能力怎样帮助旧 Worker

原 registered Worker 可查询 cwd、status、diff、staged、log、check-capabilities、check-reuse 和 check-running。调用只选固定 view，不接收任意 Git 参数、shell 或路径，读取前验证原登记、输入与 bootstrap。

这些入口使用固定 argv 和有界输出／时间，关闭外部 diff／textconv、pager、fsmonitor、lazy fetch 等可能改变只读行为的辅助功能，并处理继承 Git 环境覆盖。结果帮助解释实际现场，不产生完成证据或业务 Runtime revision。

这是原冻结环境的观察适配，不是新原生 Worker 普通 Bash 的统一许可列表，也不把 arbitrary shell 变成已沙箱化程序。新原生工具路径按其平台与专业任务工作。

## 12. Main 搬迁为什么需要窄事务

### 12.1 改路径不能绕过权威检查

项目目录移动后，普通 Writer 拒绝不同控制根是必要行为；直接修改 root 字段或增加 skip-authority 会破坏单一权威。原搬迁能力因此处理旧／新 Main、实际工作区和每个执行目录映射。

Main 可正常使用 Git 完成实际 worktree move／repair 并判断现场；系统按原搬迁意图和实际路径处理映射，不登记、计算或比对 Git OID 来认证搬迁。旧／新现场有冲突或原意图缺失时保留现场并反馈具体未知。

### 12.2 原意图、原事务与指针恢复

原 CLI 先持久化不可变搬迁意图，包含准确源 Snapshot、前后目录注册与原检查点映射；持原工作区和相关旧 launch lease，再经 Runtime 的窄事务修改允许的坐标并更新指针／检查点。

响应丢失时沿同一意图和原 marker／journal 继续，不重建源 Snapshot 来追认。Runtime 并发变化、实际工作区冲突、仍活动的旧 lease 或尚未完成的交付／检查点，保持现场并反馈实际恢复条件。

移动证据 locator 不改原回执、原工作区或实际检查结论，也不刷新成一项新执行；旧记录内的散列只作为历史数据读取，不再重算或验证。原记录仍有可读历史来源，其对当前资格的效力由原消费者判断。

### 12.3 当前组合边界

原 PlanRebind 移动执行记录与历史记录中的 ToolCheckReceipts、OwnedCheckReceipts 读取路径。Runtime 的窄事务只接受准确旧 Main 相对位置在新 Main 中的对应位置；按原意图映射这些 locator，保留原责任、WorkerCheck 索引、命令、Profile 和已收事实，不通过重算散列或 Git OID 验证搬迁。路径越界、任意新位置或其他事实变化仍拒绝，不要求作者删除工具事实来换取通过。

原回执与其日志可在准确新位置继续读取；历史是否被外部原地改动不能仅凭路径和版本独立认证。它仍描述旧 checkout 上发生的检查；新的实际 checkout 不凭这份可读历史取得复用资格。新的回执类型和复杂历史是否可接续，按原领域消费者、实际可用内容及原意图判断；本路径服务已有记录的显式恢复，不成为正常子代理的启动或搬迁前置。

## 13. 有限修复授权怎样减少重复打断

检查能力与批准修复是不同问题。这里保留原有限授权，是为了在明确人意范围内减少重复批准，而不把 runner 可用误作修复许可。S8 的具体专业工作与合同内容见 [问题调查](../l3-stages/L3-S8-finding-investigation.md)。

默认仍由人按原入口批准最终合同。另有明确 per-REQ 委托，或在需求绑定时固定、由原需求批准者授权的项目政策；它们指定 reviewer、允许／禁止路径、正数合同预算及适用有效期，并关联原 REQ、Runtime 与实际责任。per-REQ 委托有明确到期要求；原绑定项目政策可不设墙钟到期时间，但仍受其绑定、内容有效性与有限预算约束。

批准前核对最终合同的实际内容、路径与授权范围、真实技术审查、原已登记支持证据及剩余预算。技术 reviewer 说明语义未变、可逆性、范围完整与依据；这些布尔字段不是机器自动证明其判断正确。

原批准事务消费预算并保留用途，同一已批准合同正常重放不重复消耗。政策或委托撤销／失效阻止后续批准，不抹去已经批准历史；需要停止在途工作时使用原暂停／撤销路径。

有限授权不包括需求变更、设计方向、破坏性动作或发布，也不免除 S9 实际独立复验、新完整 S7 和 S11。技术批准者不自动成为合格最终 verifier，实际独立性仍须维护。

## 14. 当前实现与验证边界

以下列现有实现落点，不表示已经符合无散列目标；runner、回执、复用、授权、搬迁及其 schema／后继须共同调整并按原入口验证。

| 责任 | 当前落点 |
| --- | --- |
| Profile、入口能力、观察与失败 DTO | [checkexec/profile.go](../../internal/checkexec/profile.go)、[capability.go](../../internal/checkexec/capability.go)、[fingerprint.go](../../internal/checkexec/fingerprint.go)、[failure.go](../../internal/checkexec/failure.go)；现有指纹逻辑须从 Hook 及其可达消费者摘除 |
| 原 runner 与隔离预检 | [workspace/check.go](../../internal/workspace/check.go)、[capabilities.go](../../internal/workspace/capabilities.go)、[check_linux.go](../../internal/workspace/check_linux.go) |
| 工具捕获、验证、有限复用与运行诊断 | [accepted.go](../../internal/checkexec/accepted.go)、[reuse.go](../../internal/checkexec/reuse.go)、[permissions.go](../../internal/checkexec/permissions.go)、[running.go](../../internal/checkexec/running.go) |
| Worker 工具事实与原 Writer 接收 | [check_receipt.go](../../internal/workspace/check_receipt.go)、[check_owned.go](../../internal/workspace/check_owned.go)、[workspace_check.go](../../internal/cli/workspace_check.go) |
| Main 检查与原检查点 | [integration/check_profile.go](../../internal/integration/check_profile.go)、[check_receipt.go](../../internal/integration/check_receipt.go) |
| 冻结观察与搬迁 | [workspace/observe.go](../../internal/workspace/observe.go)、[rebind.go](../../internal/workspace/rebind.go)、[workspace_rebind.go](../../internal/cli/workspace_rebind.go)、[runtime/relocation.go](../../internal/runtime/relocation.go) |
| 有限修复委托／政策 | [delegation.go](../../internal/investigation/delegation.go)、[repairpolicy](../../internal/repairpolicy) |
| 原使用入口与专业派发 | [workspace-integration](../../docs/workspace-integration.md)、[agent-dispatch](../../skills/agent-dispatch/SKILL.md) |

验证要区分结构、真实 runner 与平台。Schema 和机械测试可核对原索引、输入变化、原 capture／接收、重放、预算及恢复；真实 Linux 试验才能证明隔离实际可用；实际 Claude Code 才能证明任务、私人路径、宿主工具与 Main 接续成立。

宿主拒绝 UID 映射、缺隔离能力或被跳过的实跑，保持未执行或失败范围，不能借其他通过测试补成 PASS。显式 enabled、搬迁和冻结恢复也须覆盖相应实际组合；未验证能力保持边界清楚，不以新握手或第二执行状态机补偿。
