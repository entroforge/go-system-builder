# 验证、调查与验收报告

| 内容 | 模板 |
| --- | --- |
| 文档与交付审查 | [REV](review/REV-template.md)、[Result](review/RESULT-template.md) |
| 工程质量 | [QA](qa/QA-template.md) |
| 端到端验证 | [E2E](e2e/E2E-template.md) |
| 缺陷调查与修复 | [BUG](bugs/BUG-template.md) |
| 验收 | [ACC](acceptance/ACC-template.md) |
| 发布审计 | [审计模板](release-audits/TEMPLATE.md) |
| Foundation回放 | [说明](design-foundation/README.md) |

报告须绑定版本、证据与适用范围；模板不是PASS，定向复验不能代替完整审查轮。发布审计目录嵌套于reports不授予S7写入权限，阶段写域由Harness按语义判定。

报告中的专业结论、PASS/N/A、BUG 生命周期和轮次状态按各自模板/消费者填写，
不表示撰写者的责任进度。责任 attempt 使用
[`docs/rules/state-machine.md`](../rules/state-machine.md#21-loop-responsibility-status)
定义的五态；只读原接收回执与 Runtime 投影，不手写通用状态。
