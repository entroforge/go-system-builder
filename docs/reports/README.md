# 验证、调查与验收报告

模板源码库在此保存空白模板和可复用说明。安装到业务项目后，该目录用于保存该项目的真实报告。维护框架本身产生的带日期修复记录、验证日志和源码备份不属于这里的模板资产。

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
