# S5 阶段答卷交互原型

这是工厂维护用的隔离契约原型，对应 [L4 阶段答卷](../../blueprint/l4-mechanisms/L4-stage-form.md) 与 [L4 手续收敛](../../blueprint/l4-mechanisms/L4-procedure-reduction.md)。不在 release include 白名单内。

生产 Harness 的阶段答卷仍未实现。原型只运行在浏览器内存；不读取或修改项目 Runtime/journal，不调用 Claude、Git、合并或发布。关闭/刷新即丢弃演示。subjectVersion 是模拟冻结身份，不是实际 SHA 校验；owner 由演示角色切换器模拟，不是身份认证。

## 运行

在仓库根目录执行：

```sh
python3 -m http.server 8765 --bind 127.0.0.1 --directory tools/stage-form-prototype
```

浏览器打开 `http://127.0.0.1:8765/`。无安装依赖或外部网络资源。

## 实际可检验的交互

1. 规格审查者读卷内资料、选项和例子，在 answer 填通过；模拟保存后立即接收并说明其工作结束、等待任务审查者。
2. 切换任务审查者，仍只读自己的卷并填通过；最后一次保存立即进入演示 S06，不需要下一次 Bash、ready、手工 evidence 或 submit。
3. 重复投递不增记录；两份独占输入保留彼此回答；错误有 JSON Pointer。
4. 完整修正发现走 S02；需求变更进入等待人决定；资料版本变化拒收旧卷。

按钮只是填入模拟答案并触发保存的便捷工具；也可以直接编辑 JSON，停顿 700ms 自动保存。这不证明某次实际软件审查已经通过。合成 fixture 与“返工示例”是分支演示材料，原型不自动判断发现与业务事实是否相符。

## 模板与消费边界

`s05-template.json` 注册题目、选项、示例、角色、读序和初始答案；`engine.mjs` 只实现这个 S5 adapter 的受限校验及接收模拟，不是通用 JSON Schema validator 或生产登记器。生产的 authoring Schema、原 evidence Schema 与领域 gate 仍需实际适配。

原型示范一个阶段的逻辑汇总与两份独占输入。只有 consumer 修改汇总，不模拟多个编辑器直接覆写同一个文件。已接受回答保留事件历史与 supersedes；重复旧回答不会复活被替代的 PASS。

## 测试与未覆盖项

```sh
node --test tools/stage-form-prototype/engine.test.mjs
```

20 个行为测试覆盖自描述题目、模板注册、两人接收、即时推进、并行快照、重复/替代回执及人闸待办、条件发现、旧卷/混用基线、owner、引用与固定失败路由。浏览器另验证直接编辑后立即切换角色时，离开者的待保存答案仍被消费。

未证明：真实文件落盘/多进程并发、生产 authoring Schema、官方 Hook payload 和 additionalContext、完整 subject closure SHA/Git tree、真实独立性、原领域 gate、权限屏障、长集成、断电恢复与 S0–S11 全流程。生产上线必须逐项连接并验收，不把这些纯内存测试称为实战平台验收。
