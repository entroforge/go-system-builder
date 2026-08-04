# 需求：REQ-TEST-FIXTURE

> 名称：Transition engine test fixture
> 状态：locked
> UI impact：none

This is a synthetic locked REQ used solely by the transition engine tests.
It is not a real requirement and never enters the release tarball (the Go
`testdata` directory is excluded from packaging). It exists only so the engine
tests can exercise `transition.Apply` against a real, hashable file without
depending on any bootstrap instance document under `docs/`.
