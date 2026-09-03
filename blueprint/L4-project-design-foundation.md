# L4 — 项目级设计基础与全局设计语言（Project Design Foundation）

> 层：第四层｜机制域：设计方向发现、项目级设计内核、设计语言生成、跨 REQ 继承、表面适配、设计演化与证据回灌
>
> 上游：L1 D1～D7 与五公理；L2「单需求单周期」「先设计后实现」「债务登记」
>
> 下游：[L3-S0 需求设计](L3-S0-requirement-design.md)、[L3-S2 设计](L3-S2-design.md)、S3 契约、S5 文档验证、S7 真实验证
>
> 姊妹篇：[L4 运行时控制面](L4-runtime-control-plane.md)管理 Loop 内的权威状态与机械约束；本文管理 Loop 外、跨 REQ 长期生效的项目级设计真相。
>
> 状态：v1.0.0，目标态蓝图。本文定义设计控制逻辑，不宣称模板、Skill、Schema、UI Lab、Design Token 或视觉回归已经完成接线。

## 0. 决策摘要

### 0.1 要消灭的工作方式

当前低效实践是：先让不同 REQ 各自生成页面，再横向搜集全部按钮、弹窗、筛选器、卡片和状态，从中挑一个相对出彩的实现，最后以它为标准重写其他页面。

```text
局部自由生成
  → 页面数量增加
  → 横向发现风格漂移
  → 选一个局部“冠军”
  → 反向统一全部实现
  → 新 REQ 再次漂移
```

这条路径的问题不是审查不够认真，而是设计标准产生得太晚：

- 每个 Agent 在开始时都要重新猜品牌、审美、信息秩序和 UI 性格；
- 局部最漂亮的组件不一定代表全局最正确的设计关系；
- 后置统一只能修正表面，无法恢复页面为什么这样组织的共同逻辑；
- 返工成本随页面数量增长，并反复消耗人的注意力、上下文和 Token；
- 最终形成的是“看起来一样”，而不是“源自同一个设计思想”。

### 0.2 本 L4 的唯一目标

在第一个前端相关 REQ 之前，建立一个项目级设计基础，使人只把控少数宏观设计判断，Agent 就能据此生成、解释、评审和演化底层页面。

```text
产品真相
  → 设计内核：为什么这样设计
  → 生成语法：如何从内核推出设计选择
  → 表面配置：不同终端和角色如何变化
  → REQ 推导：当前页面如何继承、扩展或例外
  → 实现与证据
  → 反例回灌设计基础
```

这不是用一份规范描述已经做完的系统，而是在系统被大量实现前，建立它的设计生成器。

### 0.3 非目标

本文不负责：

- 提前画完所有页面；
- 在 L4 冻结每个颜色值、间距、圆角或组件 API；
- 用一个通用 UI 库代替项目自身的设计判断；
- 新增 S0.5、S1.5 或其他 Loop Stage；
- 让机器根据字段完整度判断审美好坏；
- 用像素一致性代替设计方向、用户价值和业务效果；
- 让所有产品表面拥有完全相同的布局和信息密度。

## 1. 统一对象模型

### 1.1 Project Design Foundation 是什么

项目级设计基础不是单一文件，而是一组有明确上下游关系的设计真相：

```text
Project Design Foundation
├── Design Kernel       设计世界、中心命题、核心张力、原则与反原则
├── Design Grammar      命题向视觉、交互、内容、图像和动效展开的生成规则
├── Surface Profiles    消费者端、运营端、移动端等表面的稳定适配方式
└── Proof Set           Style Tile、锚点页、压力页、Golden Flow/Screen 与反例
```

其下游实现是 Design System：Design Token、组件、模式、模板、UI Lab 和代码。Design Foundation 决定为什么与如何选择，Design System 承载已经做出的选择；两者不能倒置。

### 1.2 “点、线、面”的唯一含义

本蓝图保留“由点及面”，但只允许一种解释：

| 层级 | 权威对象 | 回答的问题 | 不是 |
| --- | --- | --- | --- |
| 点 | Design Kernel | 产品以什么设计世界和中心命题存在？ | 一个按钮、页面或交互时刻 |
| 线 | Design Grammar | 这个命题如何连续推导颜色、版式、信息、交互和 UI？ | 页面路由或用户流程本身 |
| 面 | Surface Expression | 同一语法如何在页面、流程、终端和角色中形成整体？ | 把所有组件做成同一种外形 |

用户旅程与服务蓝图属于 Evidence Field：它们帮助发现用户行动、心智、情绪、前后台服务和机会点；North-star Screen/Flow、页面原型和组件状态属于 Proof Set：它们证明点和线能否在真实业务中成立。两类资产都不再承担顶层设计定义。

### 1.3 权威链与优先级

| 发生冲突时 | 优先级与处理 |
| --- | --- |
| 页面实现与 Design Grammar 冲突 | 默认修页面；页面不能静默改写全局语言 |
| 组件库默认样式与 Design Kernel 冲突 | 改造、包装或放弃默认样式；UI 库不是设计权威 |
| 当前 REQ 与 Design Foundation 冲突 | 提出局部例外或全局修订，由人判断价值变化 |
| Design Grammar 无法支撑真实业务 | 回到 Kernel/Grammar 修订并评估影响，不批量打补丁 |
| Golden Screen 与当前设计基础冲突 | 先判断基准是否过期；截图不是高于设计命题的事实 |

### 1.4 合格设计内核的五个性质

顶层理念只有同时具备以下性质，才足以向底层辐射：

| 性质 | 含义 | 自检问题 |
| --- | --- | --- |
| 可生成 | 面对未见过的页面仍能给出方向 | 能否从命题推导信息顺序、强调关系和交互姿态？ |
| 可排除 | 能拒绝看似不错但不属于本产品的方案 | 能否说明默认 UI、流行风格或某参考为何不适用？ |
| 可组合 | 不依赖一张固定页面才能成立 | 能否同时指导首页、复杂任务、错误状态和后台？ |
| 可适配 | 保持内核的同时允许不同表面变化 | 能否解释消费者端与运营端为何同源但不同形？ |
| 可追溯 | 每个宏观选择能回到产品事实 | 能否说明这个设计判断由什么业务或用户依据支撑？ |

“高级、简约、科技、年轻、留白多”不具备这些性质。它们最多是待解释的感受词，不能直接成为 Design Kernel。

### 1.5 对 L1 决策与五公理的承载

| 本 L4 设计 | 承载的 L1 决策 | 公理检验 |
| --- | --- | --- |
| Kernel、Grammar、Profiles、Proof 和决策理由落盘，对话不作权威 | D1 权威外置 | 消费公理、传达公理：后续 REQ 与 Agent 能读取并重建设计理由 |
| 在项目理解、首个 UI REQ 和新 Surface 等自然路径检查 Foundation | D2 观测在自然路径 | 消费公理：检查结果直接决定继承、补建或修订；L5 才定义真实接线 |
| 缺失时给出证据、候选方向和下一步，不默认阻断普通工作 | D3 门是顾问；D5 三级强制 | 成本公理：先提示词和引导性产物，观测到稳定失效后才升级结构或机械门 |
| Thesis、Tensions、Laws、Derivation Note 的结构本身承载设计追问 | D4 引导性产物 | 分工公理：Agent 作语义判断，人只裁决价值，机器以后只核对可公证事实 |
| Agent 发散与推导、独立 Critic/验证、人的方向与发布确认 | D6 三方收敛 | 分工公理：产出者不独占签收权，审美价值不交给字段完整度判断 |
| 有界回退、失败层级、未决项和返工信号可观察 | D7 收敛可观测 | 成本公理：回退只重做受影响下游，不允许无限发散或全量重来 |
| 方法分别借鉴品牌团队、设计语言、Design System、Token 和视觉测试的成熟原型 | D4/D6 | 原型公理：每个对象都能指出现实团队中的对应角色、产物或验证动作 |

传达公理贯穿整条生成链：页面不仅引用规则，还通过 Design Derivation Note 携带“依据—取舍—后果”。当前 L4 只给出机制蓝图；D2 的不可绕过接线、D5 的结构与强制升级、D6 的独立派发均须在 L5 用真实能力验证，不能在本文冒充已实现。

## 2. 项目级设计基础的生命周期编排

### 2.1 触发时机

Design Foundation 位于 Loop 之外，但 Agent 必须在以下时机主动检查它：

- 首次理解一个新项目，确认产品包含用户可见界面；
- 第一个 `UI impact=changed` 的 REQ 启动前；
- 新增消费者端、运营端、移动端或其他产品表面；
- 页面不断使用默认 UI、一次性样式或重复组件；
- 不同 REQ 对相同语义产生不同视觉与交互表达；
- 现有风格无法解释新的业务价值或品牌方向。

纯后端、基础设施或不改变用户可见行为的 REQ，只读取已有基础，不启动完整定向。

### 2.2 单一主流程

设计基础只遵循以下七步，不再并存多套工作流：

| 步骤 | 核心问题 | Agent 的主要动作 | 产出 | 人的参与 |
| --- | --- | --- | --- | --- |
| F0 识别 | 是否需要项目级设计基础或修订？ | 理解项目与产品表面；检查已有设计真相 | Foundation 状态判断 | 无需拍板 |
| F1 发现 | 产品真正值得被设计的是什么？ | 挖掘产品真相、用户关系、服务仪式、品牌证据和类别惯例 | Evidence Field | 补充事实，纠正误解 |
| F2 定向 | 可以形成哪些本质不同的设计世界？ | 建立 2～3 个 Design Kernel 候选和 Style Tile | Direction Set | 选择价值与审美方向 |
| F3 收敛 | 哪个顶层命题将长期约束产品？ | 评审取舍、反例和扩展性；形成正式 Kernel | Human-confirmed Design Kernel | 确认命题、张力、反原则 |
| F4 编译 | 命题如何变成可生成的设计语言？ | 编写 Design Grammar 和 Surface Profiles | Grammar + Profiles | 只判断关键关系 |
| F5 证明 | 这套语言能否承担真实业务和复杂状态？ | 制作 Style Tile、锚点页、压力页和 Golden Flow | Proof Set | 宏观验收，不逐像素指导 |
| F6 发布 | 后续 REQ 如何稳定继承和演化？ | 写入权威文档，登记未决项、例外和版本 | Current Foundation | 确认发布或修订 |

这七步可以在一次或多次对话中完成，但不能颠倒成“先批量做页面，再从页面归纳内核”。

### 2.3 单一主线中的有界回退

单一主流程代表只有一条权威因果链，不代表瀑布式只进不退。证据推翻假设时应精确回到产生错误的层级：

| 失败信号 | 回退位置 | 修正什么 |
| --- | --- | --- |
| 候选方向只是同一风格换皮，或都依赖行业套话 | F1 | 补产品证据、服务观察、类别反转与约束 |
| Thesis 好听，但无法推出跨维度判断 | F2/F3 | 重写关系、张力、Laws 和 Anti-principles |
| Grammar 退化成数值表，或各维度互相冲突 | F3/F4 | 修正顶层法则或维度间的因果关系 |
| Anchor Screen 成立，Stress Screen 失效 | F4 | 补不变量、受控变量、选择规则与 Profile |
| 所有表达都一致，但不改善理解、信任或任务结果 | F1/F3 | 重审用户关系、产品真相与中心命题 |

回退后只重做受影响的下游，不把所有步骤重新走一遍。这样既允许 [Double Diamond](https://www.designcouncil.org.uk/resources/framework-for-innovation/) 式的发散、收敛和反复验证，也避免多套流程争夺权威。

### 2.4 三次人的高杠杆确认

人只需要承担三次高杠杆裁决：

1. **方向确认**：哪个设计世界最符合产品价值，接受什么取舍；
2. **内核确认**：Design Thesis、核心张力、原则与反原则是否代表长期方向；
3. **发布确认**：锚点和压力场景是否证明该基础足以被后续 REQ 继承。

其余研究、发散、翻译、语法生成、页面推导和一致性检查由 Agent 完成。人不负责逐项选择颜色、间距、按钮和组件形态。

这三次确认是提示词与协作协议中的对话职责，不是 Runtime 阶段或机械 Gate。

### 2.5 对话层就绪条件

Design Foundation 可以进入 F6 发布，不要求底层资产齐全，但至少必须满足：

- 有一条可生成、可排除的 Design Thesis；
- 有明确的产品依据、用户关系和 2～3 组核心张力；
- 有 3～7 条带取舍和反例的 Design Laws；
- 视觉、交互、内容、图像和动效至少已形成方向性语法；
- 至少一个主表面 Profile 已定义；
- Style Tile 与一个锚点页能共同证明方向；
- 一个高密度、异常或失败场景能证明语言不会只在“漂亮页面”中成立；
- 未决项和设计债务显式记录。

这是 Agent 与人的语义质量线，不是当前 Runtime 的机械 Gate。

## 3. F1：发现产品真正的设计来源

### 3.1 从产品真相开始，而不是从参考图开始

Agent 应先建立 Evidence Field，并把输入分为六类：

| 来源 | 要找什么 | 为什么重要 |
| --- | --- | --- |
| 产品本质 | 产品真正出售或提供的是商品、判断、秩序、身份、陪伴还是能力 | 决定设计世界的核心角色 |
| 用户关系 | 用户希望被服务、被指导、被赋能、被保护还是被激发 | 决定信息和交互姿态 |
| 业务证据 | 数据、流程、专业标准、服务能力、供应链或历史资产 | 让独特性来自真实能力 |
| 服务仪式 | 线下咨询、交付、确认、解释、包装、售后等真实动作 | 提供可转译的节奏和品牌行为 |
| 类别惯例 | 同类产品通常如何组织、表达和说服 | 识别必须保留的可用性与可以反转的默认 |
| 约束条件 | 终端、性能、内容、法规、可访问性、数据时效和运维能力 | 防止建立无法落地的审美世界 |

Evidence Field 必须区分：已确认事实、Agent 推断、外部参考、未知项。参考产品只能提供方法或关系，不能被冒充为本项目事实。

[NN/g 的 Journey Mapping 框架](https://www.nngroup.com/articles/journey-mapping-101/)可用于组织角色、场景、阶段、行动、心智、情绪和机会；涉及完整服务时再补前台、后台和支持过程。它是发现设计来源的工具，不是视觉风格模板。

### 3.2 品牌考古与服务观察

成熟设计团队通常不会只问“想要什么风格”，而会观察品牌已经如何行动。Agent 应寻找：

- 产品在哪个时刻最值得被信任；
- 用户愿意为哪种专业能力或服务方式付出溢价；
- 线下体验中有哪些稳定的解释、等待、确认或交付仪式；
- 哪些内容、材料、数据和语言是竞争者难以复制的；
- 品牌面对失败、延迟和不确定性时应表现出什么态度；
- 产品最不希望建立哪种用户关系。

[Aesop 的数字化案例](https://work.co/clients/aesop/)说明了这种路径：先提炼门店服务、包装、内容和品牌气质，再把它们转成数字体验、内容规范和模块化系统。可借鉴的是“从真实体验提炼数字语言”，不是复制其版式和字体。

### 3.3 参考使用协议

每个外部参考都要拆成四层：

```text
参考表面：它看起来怎样
参考机制：它用什么关系解决什么问题
可迁移条件：本项目是否拥有相同问题和支撑能力
本地转译：如何形成属于本产品的表达
```

禁止只保存截图、颜色和组件外形。Agent 引用案例时必须说明借鉴的是信息秩序、服务仪式、品牌角色、反馈节奏还是系统治理。

## 4. F2～F3：建立并确认 Design Kernel

### 4.1 Kernel 的组成

Design Kernel 是项目级设计基础的核心，应保持短、稳定、有判断力：

| 内容 | 定义 |
| --- | --- |
| Design Worldview | 产品如何看待用户、行业、服务和价值 |
| Relationship Model | 产品以什么角色与用户相处 |
| Design Thesis | 一句能够持续生成与拒绝方案的中心命题 |
| Core Tensions | 2～3 组必须长期处理的张力及其偏向 |
| Design Laws | 3～7 条跨维度设计法则 |
| Anti-principles | 明确拒绝的设计世界、行业惯性和表达方式 |
| Signature Relations | 少数可被识别、能够跨页面复现的关系或品牌动作 |

### 4.2 Design Thesis 的生成公式

推荐句式：

```text
我们不是把【产品】设计成【类别默认角色】，
而是把它设计成【独特关系/世界】；
依靠【真实业务或品牌证据】，
让用户在【核心任务】中获得【目标状态/能力】。
因此产品始终【关键设计姿态】，并拒绝【反向边界】。
```

例：

```text
我们不把产品设计成功能和促销的集合，而把它设计成一位
有判断力、克制且愿意解释的专业服务者；依靠透明依据和可兑现流程，
让用户在关键选择中保持从容与掌控。因此信息先建立依据再邀请行动，
决定可预览、可确认、可恢复，并拒绝用装饰、压迫式强调和模糊承诺制造价值感。
```

这个例子不是通用答案，而是展示合格命题如何同时包含关系、证据、用户结果、生成规则和排除规则。

### 4.3 用张力替代空泛形容词

设计方向不是选择一个孤立词，而是决定如何长期处理矛盾：

| 空泛词 | 应转化为的张力问题 |
| --- | --- |
| 高级 | 克制与丰盛如何分配？价值来自材料、秩序、服务还是证据？ |
| 专业 | 权威与可理解性如何平衡？依据何时出现、解释到什么程度？ |
| 亲和 | 温度与可信度如何共存？文案、图像和反馈如何避免轻浮？ |
| 极简 | 删除什么、保留什么？复杂业务如何不被隐藏？ |
| 科技 | 技术是被展示、被解释还是退到幕后？用户是否仍有掌控感？ |

每组张力必须记录：当前偏向、不可越过的边界、在哪些场景允许反向调整。

### 4.4 Design Law 的格式

原则必须能生成想法、评审方案并拒绝不合适的实现。USWDS 将设计原则定位为团队的共同目标和决策评估视角，其成熟度材料也强调原则应帮助生成想法并对不合适提议说“不”。本项目中的每条 Design Law 应包含：

```text
名称：短而可记忆
信念：我们相信什么
依据：由什么产品/用户事实支撑
设计后果：它改变哪些信息、视觉和交互关系
适用条件：什么时候优先使用
代价：为了它愿意牺牲什么
Do：正向示例
Don't：反例和禁止的捷径
证明：在哪个锚点或流程中可观察
```

示例：

| Law | 设计后果 |
| --- | --- |
| 先建立依据，再邀请行动 | 关键 CTA 之前有足够的差异、条件和后果；强调色不抢在证据前出现 |
| 强调是一种稀缺资源 | 页面只保留一个主要焦点；颜色、尺寸和动效不能同时争抢注意力 |
| 复杂性应被组织，而不是隐藏 | 高密度页面通过分层、渐进披露和稳定结构降低负担，不删除必要业务事实 |
| 承诺必须可追踪 | 提交、等待、变化、失败和恢复都说明状态、责任和下一步 |
| 品牌表达服务于关键时刻 | 高频操作快速克制；低频且有意义的确认、交付或完成才承担更丰富表达 |

### 4.5 发散出真正不同的方向

Agent 应提出 2～3 个不同的 Design Kernel，而不是同一页面换色。方向差异至少发生在三项：产品角色、信息秩序、用户关系、审美世界、交互姿态、品牌表达位置。

可使用六种创造力来源：

1. **证据放大**：把独有数据、流程和专业标准转成设计语言；
2. **仪式迁移**：把线下服务中的解释、等待、确认和交付关系转成数字行为；
3. **类别反转**：保留可用性，反转行业中不符合本产品价值的默认顺序；
4. **跨域类比**：向编辑出版、档案、酒店、医疗、仪器、交通等相邻领域借关系，不借皮肤；
5. **约束转资产**：把小屏、性能、数据时效、人工服务等限制变成节奏和识别；
6. **反例先行**：从错误、拒绝、过期、无权限和恢复状态判断品牌人格是否真实。

### 4.6 Style Tile 是方向工具，不是设计基础

[Style Tiles](https://styletil.es/)位于过于抽象的 Moodboard 与过于具体的完整页面之间，通过字体、颜色和真实界面元素建立设计者与决策者的共同视觉语言。

每个候选方向至少展示：

- 版式与字体关系；
- 色彩角色和强调策略；
- 页面背景与表面层级；
- 图像、图标、线条和材质；
- 主要/次要行动与状态表达；
- 一段真实内容；
- 一个成功状态和一个失败/空状态；
- 对应的 Thesis、Laws、Anti-principles 与风险。

Style Tile 用来比较设计世界，不用来冻结每个底层值。人选择的是“哪套设计思想最适合产品”，不是“哪张图最好看”。

### 4.7 方向评审

候选方向从六个维度评审：

| 维度 | 关键问题 |
| --- | --- |
| Truth | 是否源自真实产品、用户和业务证据？ |
| Distinctiveness | 去掉 Logo 后是否仍有可识别的关系和气质？ |
| Generativity | 是否能指导未见过的页面与复杂状态？ |
| Elasticity | 能否在消费者端、后台和高密度场景中变化而不丢失内核？ |
| Usability | 品牌表达是否增强理解、信任和掌控，而不是妨碍任务？ |
| Feasibility | 当前内容、技术和运营能力能否持续兑现？ |

方向结论必须记录选择理由、接受的代价、被拒方向的原因和可保留元素，不能只记录“A 获胜”。

## 5. F4：把 Design Kernel 编译为 Design Grammar

### 5.1 Grammar 描述关系，不罗列值

Design Grammar 的任务是让 Agent 面对新场景时知道如何选择。规则采用统一结构：

```text
因为【Kernel/Law】
所以在【场景/任务】
优先采用【设计关系】
避免【冲突关系】
允许【受控变化】
通过【Proof】判断是否成立
```

“标题 32px、圆角 12px、主色 #123456”不是 Grammar；“标题与正文保持强编辑层级，但强调色只用于承诺型行动”才是。前者属于 Token，后者决定 Token 为什么存在。

### 5.2 Grammar 的九个维度

| 维度 | 顶层要控制的关系 |
| --- | --- |
| Information | 信息先后、证据、解释、渐进披露与决策负担 |
| Composition | 焦点、阅读路径、网格、密度、留白和节奏 |
| Color | 基础环境、内容、行动、状态和品牌强调之间的角色关系 |
| Typography | 声音、层级、长短文本、数据和元信息的关系 |
| Shape & Surface | 边界、层次、材质、圆角、阴影和空间深度的共同性格 |
| Image & Icon | 图像真实性、构图、裁切、图标和信息标记的共同语法 |
| Interaction | 默认、选择、确认、取消、权限、错误和恢复的行为姿态 |
| Content | 用词、句式、解释程度、承诺、禁语和证据表达 |
| Motion | 注意力、因果、空间关系、频率、节奏和品牌时刻 |

每个维度只记录跨场景稳定的关系。页面专属布局、一次性营销主题和局部组件细节留在模块设计包。

### 5.3 一条法则如何纵向编译

每条重要 Design Law 都必须留下这条可追溯链：

```text
Evidence → Thesis / Law → Grammar Rule → Surface Adaptation → Proof → REQ Derivation
```

以下不是通用视觉答案，而是展示“先建立依据，再邀请行动”如何由点连成线、再展开成面：

| 层级 | 消费者端表达 | 运营端表达 |
| --- | --- | --- |
| Information | 先解释差异、条件和后果，再出现承诺型 CTA | 先呈现来源、状态和影响范围，再允许批量执行 |
| Composition / Color | 证据区、判断区、行动区形成明确顺序；强调色不提前抢夺注意力 | 高密度不等于弱层级；危险操作只在上下文完整时获得高强调 |
| Interaction / Content | 决定可预览、可确认、可恢复；承诺不用模糊或压迫式措辞 | 操作显示对象、权限、影响量和可逆性；日志与反馈可追踪 |
| Motion | 动效解释选择到结果的因果，不制造紧迫感 | 高频任务尽量安静，仅在跨状态或高风险结果中强化反馈 |
| Proof | 核心选择流程 + 支付/提交失败与恢复 | 批量操作 + 权限不足、部分失败与撤销 |

两种表面不必同形，但每项差异都能回到同一条 Law。若无法回指，就不是受控适配，而是新的局部风格。

### 5.4 不变量、受控变量和选择规则

Design Grammar 必须同时定义：

- **Invariants**：所有表面都应保持的设计关系，例如强调稀缺、状态可解释；
- **Variants**：允许因表面、任务或密度变化的部分，例如留白、字号、导航、表格密度；
- **Selection Rules**：在什么条件下选用哪种模式，例如探索、判断、承诺、等待、恢复；
- **Exceptions**：为什么可以偏离、影响范围、复查条件和是否可能晋升为全局。

没有 Selection Rules 的组件库，会把选择重新推给实现 Agent；没有 Variants 的设计语言，会把一致性误解为所有页面同形。

### 5.5 Surface Profile 保证同源而不同形

Surface Profile 不是为每个终端另建一套审美，而是声明同一 Grammar 在特定产品表面的表达带宽。每份 Profile 至少回答：

- 主要用户、任务模式和产品与人的关系；
- 信息密度、解释深度、决策速度和错误成本；
- 导航、输入、反馈与恢复的主要姿态；
- 哪些 Kernel/Grammar 不变量必须保留；
- 哪些视觉、布局、内容和动效变量可以调整；
- 品牌表达可以出现在哪里、强到什么程度；
- 用哪个对比场景证明它与其他 Surface 同源。

新增 Surface 时先继承现有 Kernel/Grammar，再声明差异；不能以“后台需要更高密度”或“移动端空间有限”为理由从零发明风格。

### 5.6 Design Token 的正确位置

Token 是 Design Grammar 的实现载体，不是顶层设计的来源：

```text
Design Law
  → Semantic Role
  → Primitive Token
  → Semantic Token
  → Component Token / Theme
  → Platform-specific code
```

[DTCG 2025.10](https://www.w3.org/community/reports/design-tokens/CG-FINAL-format-20251028/)提供了跨工具表达 Token 的稳定社区规范；[USWDS](https://designsystem.digital.gov/design-tokens/)展示了如何用有限的离散选择减少任意值和沟通成本。具体格式、命名和构建接线属于 L5。

## 6. F5：用最少样本证明可辐射性

### 6.1 为什么不直接铺全部页面

顶层设计需要证明，但证明不等于批量实现。F5 只建立一组最小而有张力的 Proof Set：

| 证据 | 证明什么 |
| --- | --- |
| Style Tile | 设计世界和视觉语言是否一致 |
| Anchor Screen | 理想核心场景能否体现 Kernel 与 Grammar |
| Stress Screen | 高密度、长内容、错误或权限场景下是否仍成立 |
| Golden Flow | 视觉与交互语法能否贯穿关键任务和状态变化 |
| Surface Contrast | 消费者端与运营端等不同表面能否同源而不同形 |

只在 Anchor Screen 中成立的风格不具备系统资格；只有在压力和对比场景中仍能生成正确判断，才能发布为项目基础。

### 6.2 整体优先的评审顺序

评审从上到下进行：

1. 设计世界和 Thesis 是否可感知；
2. 信息焦点、层级、节奏和行动路径是否符合 Design Laws；
3. 视觉、交互、内容和动效是否形成同一人格；
4. Surface Profile 的变化是否有理由；
5. 最后才检查组件、Token 和像素细节。

上层失败时，应回退 Kernel/Grammar，不通过局部 CSS 把页面“修得像一点”。

### 6.3 人的宏观验收问题

人只回答：

- 它是否属于我们希望建立的设计世界？
- 页面为什么这样组织，能否回到同一个中心命题？
- 这种语言是否让产品真实价值更可感知，而不是只更漂亮？
- 它是否有足够独特性，同时能承受复杂业务？
- 我愿意让未来所有 REQ 在这个方向上继续生长吗？

## 7. F6：发布、继承与每个 REQ 的设计推导

### 7.1 发布后的生产关系

```text
Published Design Foundation
        ↓
Agent 读取 Kernel + Grammar + 当前 Surface Profile
        ↓
当前 REQ 形成 Design Derivation Note
        ↓
S2 产出模块 Story / Flow / Scenario / CASE / PATH / Prototype
        ↓
实现优先复用 Token、组件和模式
        ↓
视觉与真实验证
        ↓
局部例外或全局修订提案
```

### 7.2 Design Derivation Note

每个前端相关 REQ 不复制整份设计基础，只记录一份轻量推导说明：

```text
Foundation reference：继承的设计基础版本
Surface：当前产品表面
Experience role：当前页面主要承担探索/理解/判断/承诺/等待/兑现/恢复中的什么角色
Active laws：本 REQ 最重要的 Design Laws
New language：是否需要新增设计语法、模式或 Token
Exception：是否偏离全局，原因和范围
Proof：本 REQ 用什么页面/状态/流程证明推导成立
```

它的目的不是增加表单，而是迫使 Agent 在设计前说明“这个页面为什么这样长”。

### 7.3 防止事后横向返工

新的顺序是：

1. 首个 UI REQ 前发布 Foundation；
2. 每个 REQ 开始时声明继承和活跃法则；
3. 先做当前模块的一张宏观构图和一个压力状态；
4. 宏观推导成立后，再扩展其余页面和状态；
5. 底层一致性由语义 Token、组件知识和自动检查承担；
6. 发现多个页面共同失败时，才回到 Foundation 修订。

系统不再通过“搜集全部弹窗后挑最好看的”来定义弹窗。Design Grammar 先定义解释、确认、警告、承诺、恢复等语义关系；具体弹窗根据语义生成并复用相应模式。

### 7.4 局部发现如何回灌

| 发现类型 | 去向 |
| --- | --- |
| 当前页面的一次性构图 | 留在模块设计包 |
| 同一模块反复出现的关系 | 晋升为模块 Pattern 候选 |
| 多个模块/表面反复成立的关系 | 提出全局 Grammar/Token/Component 修订 |
| 与 Kernel 冲突但业务必须 | 建立有期限和范围的 Exception |
| 说明 Kernel 本身已过时 | 发起 Design Foundation breaking change，由人重新确认 |

任何 REQ 都不能静默把一个局部成功样本升级为全局规范。

## 8. Agent 与人的引导契约

### 8.1 Agent 的默认行为

当 Agent 理解项目后发现存在页面、视觉、交互、内容、图片、动效或用户可见状态，应：

1. 先检查 Project Design Foundation；
2. 判断它是缺失、待修复、可直接继承，还是无法覆盖新表面；
3. 若缺失或不足，暂停直接批量生成页面，进入 F1～F6；
4. 先提交完整判断和 2～3 个候选方向，再向人提问；
5. 只把产品价值、核心张力、审美世界和破坏性变更交给人裁决；
6. 把确认结果写入项目文档，后续 REQ 不再重复询问；
7. 若 Foundation 完整，S0 只登记继承关系，S2 再形成 Design Derivation Note 并完成模块推导。

### 8.2 提问原则

Agent 不问低信息问题，例如“喜欢什么颜色”“想要什么风格”。应使用带判断的选择：

```text
基于【产品事实】，我认为产品需要处理【核心张力】。
我形成了 A/B 两个设计世界：
A 强化【价值】，代价是【代价】；
B 强化【价值】，风险是【风险】。
我推荐 A，因为【业务和用户依据】。
需要你确认的是【不可由 Agent 推导的价值选择】。
```

一次优先只提交 1～3 个真正会改变全局方向的问题。

### 8.3 对话不是权威来源

对齐会话结束时必须写入：

- Evidence Field 中已确认事实、推断、来源与未知项；
- 已确认的 Kernel；
- 被选和被拒方向及理由；
- Design Grammar 与 Surface Profiles；
- Proof Set；
- 未决项、设计债务和例外。

后续 Agent 读取文档，不依赖聊天记忆。

## 9. 权威资产与职责边界

### 9.1 推荐资产形状

```text
docs/design/
├── DESIGN.md                      # 入口、Kernel、当前状态与继承关系
├── research/
│   └── evidence-field.md          # 产品事实、用户关系、服务观察、来源与未知项
├── design-language.md             # Design Grammar：跨维度设计关系
├── surface-profiles/
│   ├── consumer.md                # 消费者端姿态
│   └── operations.md              # 运营端姿态
├── proof/
│   ├── style-tiles/               # 候选与选定方向
│   ├── anchor-screens/            # 核心与压力样本
│   └── golden-flows/              # 关键流程和状态证据
├── decisions/                     # 方向选择、修订与破坏性变化
├── exceptions/                    # 有范围和期限的局部偏离
└── modules/<module>/              # S2 模块设计包

packages/design-tokens/            # primitive / semantic / component tokens
packages/ui/                       # components / patterns / stories
tools/ui-lab/                      # 可查询的实时实现知识
tools/visual-qa/                   # Golden Screen 与视觉回归
```

### 9.2 各资产的权威职责

| 资产 | 回答什么 | 不回答什么 |
| --- | --- | --- |
| Evidence Field | Kernel 依据什么产品、用户、服务与类别事实建立 | 最终设计命题和 UI 选择 |
| `DESIGN.md` | 为什么这样设计，当前 Kernel 是什么 | 完整组件 API 与全部 Token 值 |
| `design-language.md` | Kernel 如何生成视觉、交互、内容和动效 | 某 REQ 的页面详情 |
| Surface Profile | 不同产品表面如何同源适配 | 全局 Kernel 的重新定义 |
| Module Design Package | 当前业务的状态、路径、原型和设计推导 | 跨项目的品牌方向 |
| Token source | 可执行的语义值与平台映射 | 为什么应该有这个语义角色 |
| UI Lab/Storybook | 当前真实可用组件、变体和示例 | 产品价值和审美方向 |
| Golden Screen/Flow | 实现是否漂移、语言能否承载真实业务 | 单独证明方向一定正确 |

`DESIGN.md` 应保持短而有判断力。Google Stitch 的 [DESIGN.md](https://blog.google/innovation-and-ai/models-and-research/google-labs/stitch-design-md/)说明了便携设计上下文对 Agent 的价值；[Atlassian 的实践复盘](https://www.atlassian.com/blog/how-we-build/atlassians-design-md-is-here-what-we-learned-testing-portable-design-context-in-practice)则说明生产环境不能只依赖一份静态文档，还需要组件知识、代码约束和自动验证。本蓝图采用分层权威，避免巨型 Prompt。

## 10. 与 S0、S1、S2 的接口

### 10.1 Loop 外的首次建立

```text
Agent 理解项目
  → 识别存在前端/视觉业务
  → 检查 Foundation
  → 缺失时完成 F1～F6 并由人发布
  → 启动第一个相关 REQ
```

Foundation 可在 REQ 之间维护，但不拥有 Loop cursor，不新增 Stage。

### 10.2 S0：声明关系，不重写设计基础

S0 继续负责 Why、Direction、What、未知项和人锁定。前端相关 REQ 只需明确：

- UI impact；
- Foundation reference；
- 受影响 Surface；
- 是否需要新增语言或例外；

若 Agent 到 S0 才确认 `UI impact=changed` 且 Foundation 缺失或不足，应先返回 F0～F6 补齐并完成发布，再进入 S1；不能以 REQ 中几句风格描述替代项目级基础。当前 Runtime 不会机械阻断这件事，但提示词中的默认行为必须明确。

S0 不复制 Kernel/Grammar，也不在需求文档中重新发明品牌风格。

### 10.3 S1：保障可实现条件

S1 识别设计基础带来的技术约束，例如主题、响应式、无障碍、组件复用、媒体、内容和视觉验证环境。它不决定审美方向。

### 10.4 S2：消费、具体化、证明、回灌

S2 的架构与场景/原型双轨保持不变。涉及 UI 时新增四项语义责任：

1. 读取 Foundation 与当前 Surface Profile；
2. 形成 Design Derivation Note；
3. 用模块 Story、Flow、Scenario、CASE、PATH、Prototype 证明推导；
4. 将可复用发现作为 Foundation 修订提案，不静默修改全局。

当前 PTR-PLAN-01 并不机械检查 Project Design Foundation 或完整 UI package，因此本文不能把上述目标写成已接线硬门。

## 11. 验证与演化闭环

### 11.1 三类验证不能混为一谈

| 验证 | 问题 | 方法 |
| --- | --- | --- |
| Derivation Fidelity | 页面是否由 Kernel/Grammar 正确推导 | Design Derivation Note、设计评审、Do/Don’t |
| System Coherence | 跨页面、状态和表面是否共享同一语言 | UI Lab、语义 Token、Golden Screen、视觉回归 |
| Experience Effectiveness | 方向是否真的改善用户与业务结果 | 场景走查、可用性测试、S7 真实环境证据、业务反馈 |

像素回归只能发现实现漂移，不能判断 Design Thesis 是否正确。

### 11.2 设计基础的变更类型

| 类型 | 处理 |
| --- | --- |
| Clarification | 不改语义，仅让规则更可生成、可排除 |
| Extension | 新增表面、语法、Pattern 或 Proof，不改变 Kernel |
| Adaptation | 为新终端/角色定义受控变量 |
| Exception | 局部偏离，记录范围、理由、期限和复查条件 |
| Breaking Change | 改变 Worldview、Thesis、核心张力或 Design Laws，由人重新确认并评估所有 Surface |

### 11.3 何时回到上层

- 一个页面失败：先修当前推导或实现；
- 多个页面在同一语义上失败：修 Grammar/Pattern；
- 多个表面无法共享同一设计判断：检查 Surface Profile 或 Kernel；
- 用户/业务事实改变：重新进入 F1～F3；
- 只是截图差异：先判断基准和实现，不自动升级为方向问题。

### 11.4 观察信号

在考虑硬门之前，先观察：

- 新 REQ 是否仍反复询问相同风格问题；
- Agent 是否能解释页面为何这样组织；
- 默认 UI 和一次性样式是否减少；
- 人是否只需审查宏观方向和例外；
- 同类组件是否仍在大量横向返工；
- Surface Profile 是否能解释消费者端与后台的差异；
- 局部发现是否通过提案回灌，而不是静默污染全局。

## 12. 现实世界最佳实践的采用边界

| 实践 | 采用什么 | 不照搬什么 |
| --- | --- | --- |
| [Design Council Double Diamond](https://www.designcouncil.org.uk/resources/framework-for-innovation/) | F1/F2 发散发现与 F3/F5 收敛验证；允许根据证据回退 | 不把四阶段变成新的 Runtime Stage |
| [NN/g Journey Mapping](https://www.nngroup.com/articles/journey-mapping-101/) | 在 F1 组织角色、场景、阶段、行动、心智、情绪和机会，必要时延伸到服务蓝图 | 不把旅程图当视觉方向或用虚构人物替代真实证据 |
| [Style Tiles](https://styletil.es/) | 在 Moodboard 与整页稿之间建立可讨论的视觉语言 | 不把 Style Tile 当最终规范或完整页面 |
| [Aesop / Work & Co](https://work.co/clients/aesop/) | 从线下服务、包装、内容与品牌气质提炼数字系统 | 不复制其审美表面 |
| [Atomic Design](https://atomicdesign.bradfrost.com/chapter-2/) | 局部与整体并行验证，组件只能在页面上下文中证明 | 不把 atoms→pages 当线性生产流水线 |
| [IBM Design Language / Carbon](https://www.ibm.com/design/language/help/faq/) | 区分品牌设计语言与承载它的组件、模式、代码和工具 | 不把通用组件库当项目自身 Design Kernel |
| [USWDS Design Principles](https://designsystem.digital.gov/design-principles/) | 原则作为生成与评审决策的共同视角 | 不复制政府产品的具体原则和视觉风格 |
| [USWDS Design Tokens](https://designsystem.digital.gov/design-tokens/) | 用有限语义选择降低任意值和沟通成本 | 不让 Token 替代 Design Grammar |
| [Google DESIGN.md](https://blog.google/innovation-and-ai/models-and-research/google-labs/stitch-design-md/) + [Atlassian 实测](https://www.atlassian.com/blog/how-we-build/atlassians-design-md-is-here-what-we-learned-testing-portable-design-context-in-practice) | 给 Agent 便携的设计意图，同时保留实时组件知识与自动检查 | 不把一个巨型 Markdown 当生产系统唯一事实 |
| [DTCG Format 2025.10](https://www.w3.org/community/reports/design-tokens/CG-FINAL-format-20251028/) | 后续以跨工具格式承载 Token 和别名关系 | L4 不提前冻结实现格式 |
| [Storybook MCP](https://storybook.js.org/docs/ai/mcp/overview) | 让 Agent 查询当前组件、Stories 和测试能力 | 不用实时组件知识取代顶层方向 |
| [Storybook Visual Testing](https://storybook.js.org/docs/writing-tests/visual-testing) / [Playwright Visual Comparisons](https://playwright.dev/docs/test-snapshots) | 检查真实渲染是否漂移 | 不把像素相等当设计质量 |
| [Atlassian Motion](https://atlassian.design/foundations/motion) | 动效同时承担因果澄清与品牌表达，高频克制、关键时刻强化 | 不为所有操作增加动效 |

这些实践共同支持一个分层事实：原则负责方向，语言负责生成，Token/组件负责实现，真实页面负责证明，验证工具负责发现漂移。

## 13. 当前实现与目标态

| 领域 | 当前可确认事实 | 目标态 |
| --- | --- | --- |
| REQ | 已有 `UI impact=none/changed/unknown` | 同时引用 Foundation、Surface 与例外关系 |
| S2 | 已有架构、场景、原型和模块 UI package 设计 | 开始具体设计前消费 Foundation，形成 Derivation Note |
| 项目级设计真相 | 尚无正式 Kernel/Grammar/Surface Profile 契约 | 在首个 UI REQ 前建立并可跨 REQ 维护 |
| Agent 引导 | 主要围绕 Loop Stage 和模块设计 | 项目理解时主动识别并发起 F1～F6 |
| Design System | 仓库尚未建立统一的项目级 Token/UI Lab 接线 | 由 Foundation 驱动而不是由默认 UI 库反向决定 |
| 视觉验证 | 尚未形成项目级 Golden Screen/Flow 接线 | 分离方向验证、系统一致性和真实效果 |
| Runtime gate | 当前没有 Project Design Foundation gate | 首先采用提示词与文档，观察后再决定有限机械检查 |

### 13.1 为什么第一版不直接做硬门

审美和设计命题不是机器可以通过字段数量判断的事实。过早要求“必须存在完整 DESIGN.md”会制造空壳文档和形式主义。第一版应先证明 Agent 是否会主动切入、是否能提出有判断力的方向、是否减少返工；之后只把可机械部分下沉，例如文档存在、引用版本、Token 使用、组件重复和截图漂移。

## 14. 失败模式

| 失败模式 | 识别信号 | 修复层级 |
| --- | --- | --- |
| 形容词 Kernel | 只有高级/简约/科技等词，无法生成或拒绝方案 | 回 F1/F2 重做 Thesis 与张力 |
| 页面先行 | 开始讨论布局和组件，但 Kernel 未确认 | 暂停批量设计，完成 F2/F3 |
| 三张皮肤 | 候选方向只换颜色、字体和圆角 | 重做产品角色、信息秩序和交互姿态 |
| UI 库反客为主 | 页面像库的默认 Demo，无法解释项目独特性 | 回 Grammar，定义语义关系和 Anti-principles |
| 规范巨型化 | `DESIGN.md` 塞入全部组件和页面细节 | 按 Kernel/Grammar/Profile/Proof/Implementation 分层 |
| 一致性等于同形 | 后台被迫复制消费者端的留白和展示方式 | 补 Surface Profile 与受控变量 |
| 局部冠军治理 | 找一个最好看的组件重写所有同类 | 先判断它体现了哪条 Law，再决定是否晋升 |
| 人逐页修图 | 人不断调整 CSS 和间距 | 上移到 Kernel/Grammar 审查，补足生成规则 |
| 像素正确、方向错误 | 回归测试通过但体验无特色或不可信 | 回到 Thesis、用户验证和业务证据 |
| Agent 静默扩张全局 | 一个 REQ 新增风格并传播到其他模块 | 建立修订提案和人闸 |

## 15. 向 L5 下沉的实现顺序

本 L4 通过后，再按以下顺序设计实现规格：

1. `DESIGN.md` / Design Kernel 模板；
2. Design Grammar 与 Surface Profile 模板；
3. Agent 项目理解触发提示词；
4. Design Direction / Critic / Systemization Skills；
5. Style Tile、Anchor/Stress Screen 与 Design Derivation Note 模板；
6. REQ/S0 引用字段与 S2 消费协议；
7. Design Token 分层、命名、DTCG Schema 与构建；
8. UI Lab/Storybook 的 Agent 查询入口；
9. 组件提案、重复检测和局部例外流程；
10. Golden Screen/Flow 与视觉回归接线；
11. 仅对稳定机械事实增加 Hook/Gate；
12. 用真实项目回放验证是否减少横向返工与人工审查。

L5 的每一项都必须能回指本文中的权威对象和失败模式，不能新造另一套设计概念。

## 16. 结论

项目级设计基础的价值，不是记录系统已经长成什么样，而是让系统在还没有长出来时就拥有同一个设计基因：

```text
人把控产品的设计世界、中心命题和关键张力
  → Agent 把它编译成可生成、可排除、可适配的设计语法
  → 每个 REQ 只声明继承、活跃法则、新增和例外
  → 页面、组件和状态沿着同一语言自然展开
  → 证据发现漂移或反例，再回灌到正确层级
```

当这条链成立，人不需要再横向审查全部弹窗、筛选器和按钮来“找出一个标准”；这些底层对象从一开始就来自同一套上层设计判断。宏观设计因此真正成为对系统整体的控制能力，而不是最后一次昂贵的视觉清理。

## 变更记录

### v1.0.0 — 2026-09-03

- 基于多轮讨论从头重构项目级设计基础蓝图；
- 将多套重叠流程收敛为 F0～F6 单一主流程；
- 固定 Design Kernel、Design Grammar、Surface Profiles、Proof Set 四类权威对象；
- 明确“点、线、面”的唯一语义和顶层向底层的生成链；
- 增加方向发现、创造力来源、Design Law、Style Tile、最小证明集和 REQ 推导机制；
- 明确 Agent/人分工、S0/S1/S2 接口、验证层次、演化与失败路由；
- 引入并限定现实世界最佳实践的采用边界。
