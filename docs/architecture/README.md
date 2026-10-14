# 技术架构

- [架构模板](ARCHITECTURE-template.md)
- [数据模型](data-model/MODEL-template.md)
- [状态模型](state/STATE-template.md)
- [数据流](dataflow/DATAFLOW-template.md)

技术设计引用REQ并向[合同](../dev/contracts/README.md)交付模型与边界。品牌Foundation与交互原型属于[体验设计](../design/README.md)。

## Module relationships

Maintain `module-relations.json` during S2/S3 and include its locked fingerprint in S5 independent document verification. Start from [the template](module-relations-template.json); see [related-module verification](../related-module-verification.md). The actual declaration belongs to the installed project and is never installed as an empty default.
