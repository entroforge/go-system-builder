# 执行控制

[阶段协议](agent-protocol.md)规定S0–S11，[Loop Definition](loop-definition.json)规定合法状态与迁移，[Hook policy](hook-policy.json)和[受保护命令表](protected-commands.json)规定执行边界。

文件由本版本Harness读取，不能与旧布局的同名文件并存。运行事实仍在`.claude`；搬目录不能改写已冻结的path/SHA，也不能通过刷新指纹重签证据。
