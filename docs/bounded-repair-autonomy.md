# 有限修复授权

有限授权用于常规 S8 → S9 根因修复，默认不开启。它不替代质量门、S2 架构签署、需求修订、破坏性操作许可、完整 S7 或 S11 发布决定。普通故障先由 Driver 调查和恢复；只有具体授权不足才交人决定。

## 两种现有授权来源

项目所有者可在安装时批准可复用项目策略，系统在新 REQ 绑定时固定其准确文件、摘要、批准人、REQ 和代际。没有该策略时，真实需求批准人可在绑定后选择一次有限需求授权。两种方式任选，不为同一修复要求两份授权；已有 REQ 不自动迁移授权，账号名称和一句“继续开发”也不能代替它。

项目策略的专业字段是 `version: "1"`、批准人、不同于批准人的指定 Driver `reviewer`、字面量 `allowed_paths`／`forbidden_paths`、正数 `max_contracts`，以及可选 `expires_at`。项目策略不允许锁定需求、治理路径、不可逆工作或发布。策略变化或 REQ 修改会阻止后续新批准，不能自动续预算或有效期。

## 一次有限需求授权怎样填写

系统在原决定目录提供准确申请路径，绑定当前锁定 REQ。真实需求批准人选择此方式并确定边界后，Main 如实填写：

- `decision=delegate_bounded_repair`；未选择时保持空值。
- `reviewer`：指定实际 Driver，必须不同于人类批准人。
- `allowed_paths`／`forbidden_paths`：真实允许和禁止的有限修复范围。
- `max_contracts`：有限合同预算。
- `expires_at`：人明确选择的 RFC3339 到期时间。

[专业字段示例](examples/autonomy/repair-delegation.json)只示意这些真实选择，不是预先生效的授权。Runtime、锁定 REQ 摘要、代际、批准人身份、scope、证据 ID 和事务版本由原接收器承接。正常保存经原证据 Writer 生成原规范授权记录，不手填机器字段、不手工 evidence add。系统不生成真实人意，也不通过保存文件认证聊天真人身份。

路径是仓库内字面量文件或目录，不使用 glob、越界、符号链接组件或仓库根通配。完整 prospective_scope 必须落在允许范围内；若一个目录覆盖被禁止的子路径，应按真实完整修复面收窄。边界不足时准备一份完整修订决定，不要求逐文件批准。绑定、REQ 字节或代际变化后旧申请不能迁移成新授权。

## 每份最终合同的 Driver 技术审查

先完成 Case 因果闭合、完整范围和单元断言／依赖检查。保存专业合同后，系统固定最终呈现稿，并沿已绑定项目策略或唯一有效原授权，提供指定 Driver 的原技术审查申请。[专业字段示例](examples/autonomy/repair-technical-review.json)包含：

- 实际 `decision=approve_bounded_repair`；不能由已有授权或保存动作默认填入。
- `requirements_unchanged`、`business_semantics_unchanged`、`reversible`、`scope_complete` 四项真实专业判断。
- 选择真实、已登记、当前代际且原字节仍一致的 `evidence_refs`，并给出 `rationale`。

这些字段不是系统语义证明；Driver 按本合同实际边界解释入口、事务、兼容性、完整文件范围与可逆性，不无条件增加无关实验。未知或改变的业务语义不能沿本路线批准。指定 Driver 可以是承担该原责任的 Main；未经指定的 Main 负责组织与忠实记录，不能替代本人技术判断。不新增强制 Worker 角色或第二 reviewer；人类授权人与技术审查者仍按原约束区分。当前申请应明确显示实际指定责任，不能把所有 Main 会话都视为指定 Driver。若指定 Driver 就是 Main，由 Main 填写并保存原申请；否则 Main 交付准确固定最终稿与原审查字段，指定 Driver 本人返回四项判断、证据与理由，Main 忠实记录并保存原申请。Worker 保存事件不消费 Main 的申请，不另建 Worker 问卷或第二报告。

系统承接 Runtime／Case／Contract、指定 reviewer、授权引用、最终稿 `approval_hash`、scope 和证据登记，再调用原批准消费者。作者不复制 SHA、全局 revision、binding-policy 摘要或证据封套，也不执行批准命令。多份现有授权存在真实歧义时，Main 先处理准确原来源，不任选其一或新建授权登记表。

最终批准继续核验有效期、预算、保护路径、完整范围、证据字节、语义判断和当前 Case 基线。批准事务原子记录人类授权来源、Driver 审查与预算消耗。一个 grant 本身不批准合同；同一合同同答重试不重复消费，新的合同才花一次预算。合同或 Case 变化后旧技术决定不能自动签新稿。

## 逐稿真人批准与恢复

没有现成有效授权时，系统在同一原决定目录准备当前最终稿的真人批准申请，Main 仅如实记录实际人类选择和理由。普通逐稿批准不再额外要求有限 grant。过期、预算耗尽、范围不足或缺失授权不能被自动续期；可以由原人类决定实际新边界或逐稿批准。

已登记的完整原授权／审查 DTO 与低层 `runtime evidence add`、`runtime investigation contract approve` 接口保留给原冻结记录及内部适配恢复，不是正常作者的额外手续。恢复不得把技术审查导入为人类授权，不得重签旧记录。已批准合同是不可变历史；撤销 grant 阻止后续新批准，不自动取消已开始的责任，紧急停止应处理原活动责任。

## 安装与其他边界

在静止边界安装到下一 REQ，保留项目 Skills。旧二进制不保证理解新增权威元数据；回退需要兼容 Runtime／journal 及控制资产，不能只换可执行文件，也不覆盖活动 REQ 工作区。

普通 Builder 集成仍默认合并前后检查。只有明确审查过取舍、理由与失败恢复的计划才能选择现有 `integration_check_mode=post_merge`；不全局默认，不改活动 manifest，不缓存 PASS，不减少必需验证。修复后仍需独立复验及新完整 S7。
