# 需求：REQ-CLI-FIXTURE

> 名称：CLI runtime test fixture
> 状态：locked
> UI impact：none

This is a synthetic locked REQ used solely by the CLI runtime tests. It is not
a real requirement and never enters the release tarball (the Go `testdata`
directory is excluded from packaging). It exists only so the CLI tests can
exercise `runtime transition` against a real, hashable file without depending
on any bootstrap instance document under `docs/`.
