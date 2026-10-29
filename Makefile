GO ?= go
HARNESS_BIN ?= .claude/bin/loop-harness
# Full CLI integration coverage exceeds Go's default ten-minute package limit
# on slower hosts. Keep both normal and race runs complete and bounded.
TEST_TIMEOUT ?= 30m
# Integration packages spawn their own Git/check processes. Bound package
# concurrency to the configuration used by the complete regression run.
TEST_PACKAGES ?= 2
# PR checks have their own bounded budget. Full verification keeps TEST_TIMEOUT.
CI_TEST_TIMEOUT ?= 5m
CI_RACE_PACKAGES = ./internal/runtime ./internal/recovery ./internal/filelock ./internal/fileview ./internal/schema ./internal/checkexec ./internal/controller ./internal/hook ./internal/audit ./internal/submission

.PHONY: build build-all manual fmt-check vet test test-race verify doctor health platform-smoke platform-smoke-required validate test-ci test-ci-core test-ci-smoke test-ci-race ci-verify ci-verify-full doctor-staged release-graph release-graph-validate release release-checksum release-list clean-release

# Local dev build (host platform only). Used by `verify`, `ci-verify`, and
# other targets that need a runnable binary on this host.
build:
	mkdir -p $(dir $(HARNESS_BIN))
	$(GO) build -o $(HARNESS_BIN) ./cmd/loop-harness

# Regenerate the checked-in, agent-facing Manual from the executable Loop
# Definition and the compiled guard/action documentation registries.
manual:
	$(GO) run ./cmd/loop-harness manual --root . --target loop-harness.md

# Cross-compile the Harness for every release platform. CGO is disabled so
# each binary is statically linked against the Go runtime and has no host
# libc dependency; -trimpath strips the build host's working directory for
# reproducible builds; -s -w strips symbol/debug tables to keep the
# tarball small. Output names match INSTALL.md's uname-based dispatch.
BUILD_DIR ?= dist/bin

build-all:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GO) build -trimpath -o $(BUILD_DIR)/loop-harness-darwin-amd64 ./cmd/loop-harness
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -o $(BUILD_DIR)/loop-harness-linux-arm64 ./cmd/loop-harness
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 $(GO) build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/loop-harness-darwin-arm64      ./cmd/loop-harness
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/loop-harness-linux-amd64       ./cmd/loop-harness
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/loop-harness-windows-amd64.exe ./cmd/loop-harness

fmt-check:
	@test -z "$$(gofmt -l cmd internal tests)" || { gofmt -l cmd internal tests; exit 1; }

vet:
	$(GO) vet ./...

test:
	$(GO) test -p $(TEST_PACKAGES) -timeout=$(TEST_TIMEOUT) ./...

test-race:
	$(GO) test -race -p $(TEST_PACKAGES) -timeout=$(TEST_TIMEOUT) ./...

# No existing test is deleted or disabled in full verification. -short also
# leaves the audit stress test to the complete run.
test-ci: test-ci-core test-ci-smoke

# Discover new packages automatically; the excluded scenario packages have
# explicit critical-seam coverage below, and system suites run in full mode.
test-ci-core:
	@set -e; \
	packages=$$($(GO) list ./...); \
	packages=$$(printf '%s\n' "$$packages" | sed -E '\@/internal/(cli|integration|investigation)$$@d; \@/tests/system(/|$$)@d'); \
	test -n "$$packages"; \
	$(GO) test -short -p $(TEST_PACKAGES) -timeout=$(CI_TEST_TIMEOUT) $$packages

# Keep real native Start/Stop reception, author/source failures, tool freedom,
# human release authority, integration failure/resume and approval boundaries.
# Use anchored names so a similarly named slow scenario is not added by accident.
test-ci-smoke:
	$(GO) test -short -p $(TEST_PACKAGES) -timeout=$(CI_TEST_TIMEOUT) ./internal/cli -run '^Test(DesignNormalEntryDerivesMachineFactsFromTwoProfessionalAnswers|S2PendingOutletRetainsActualGapAcrossObservation|S2MissingQuestionnaireRestoresOriginalFilesOnMaterialChange|S3PreparationFailureSurvivesReadAndClearsAfterActualRepair|S2RecoveryKeepsPreviouslyIssuedTemplate|S2CLIRecoverUsesSameDiskAuthorityAsOriginalSave|S2GuidanceDeliveryReducesRepeatsAndRestoresChangedContext|S2ReceivedProjectionKeepsActualGapWithoutStaticReceptionMissing|S2ReadAndUnknownEventsNeverReceiveExistingAnswer|S2FailedAndUnchangedToolsDoNotRestoreMaterials|S3RepairDeliversNextPathsInSamePostResponse|S3PlanningObserverRequiresActualAnswerChange|QuestionnaireObserverReceivesScriptAndCustomToolSaves|PlanningObserverMissingPacketWaitsForCommitAndPreservesReadChange|S2ScopeGuidanceExposesBatchBeforeSubmission|S2PendingBatchRecoveryKeepsBoundSource|S2LargeGlobalModuleSetPreparesS3|RegisteredCustomToolSaveReachesOriginalDesignReceiver|BuildInfoDoesNotNeedProject|InitDerivesHookMetadataAndCreatesJournals|NativeS5.*|NativeS6StartStopReceivesWithoutActivationOrSaveHook|NativeS6ReceiverSourceFailureBelongsToMain|NativeS6RecoveredGitDeliveryDoesNotNeedAuthoringCaches|S3RegistrationPreservesAcceptedSourceClosure|NativeS9ActualWorktreeStopReceivesWithoutLauncherOrPlatformIdentity|NativeS9CanceledPreparationPreservesOriginalSource|NativeS9MalformedProfessionalArtifactIsNotReceived|NativeReviewResume.*|BoundHookObservesWrongDirectoryWithoutToolACL|DryRunMissingRuntimeDoesNotInitialize|MainReviewPlanQuestionnaireSaveUsesOriginalRegistrationAndReplay|StageAnswerConsumeAcceptsIssuedS7AnswerWithoutAgent|SavedReviewResultHookRejectsUnissuedPathWithoutRuntimeWrite|S1NormalFourAnswersDeriveOriginalMachineFacts|S4OriginalSavePreparesS5WithoutMainMetadataOrConfirmation|S7MainPreparesAllDispatchedQuestionnairesInOneOriginalHandoff|S7BudgetNormalApplicationRecordsTwoActualHumanChoicesAndReadOnlyReplay|S7BudgetStatusDoesNotPrepareDecisionOrGateNormalAcceptance|S8ContractHumanChoiceNormalHookWithoutMachineInput|S8ExplicitGrantAndDriverChoiceNormalHookWithoutMachineInput|S10OriginalRegistrationKeepsStatusGateAndTransitionConsistent|S10QuestionnaireHookUsesPreparedManifestAndMainProducer|S11QuestionnaireHookDeliversVerifiableS10PackageAndKeepsDecisionHuman|HookBudgetTimeoutKeepsListenerNonBlocking)$$'
	$(GO) test -short -p $(TEST_PACKAGES) -timeout=$(CI_TEST_TIMEOUT) ./internal/integration -run '^Test(CheckpointStore.*|Integrate.*|CanonicalSurfaceEarlyInspectionFailureDoesNotBlockSuccessfulRevision|IntegrationPreservesChangesWrittenByChecksAndResumesWithoutRemerge|TimeoutKillsDescendants|SuccessfulShellCannotLeaveBackgroundWriter|IntegrationLocksGitCheckoutWhenEvidenceRootDiffers)$$'
	$(GO) test -short -p $(TEST_PACKAGES) -timeout=$(CI_TEST_TIMEOUT) ./internal/investigation -run '^Test(ApprovalArtifactCleanupRetainsPendingReferencedOrChangedBytes|ApproveContract.*|DelegatedApproval.*|TechnicalReviewCannotMasqueradeAsHumanApproval|NativeS8StopReceivesOriginalCaseAnswerWithoutGenericPlanOrActorAlias|S8TypedFindingSourceHandoffNeedsNoIndexAlias)$$'

# Instrument the shared-state/locking/process boundaries, rather than running
# every serial CLI scenario again under race instrumentation.
test-ci-race:
	$(GO) test -race -short -p $(TEST_PACKAGES) -timeout=$(CI_TEST_TIMEOUT) $(CI_RACE_PACKAGES)
	$(GO) test -race -short -p $(TEST_PACKAGES) -timeout=$(CI_TEST_TIMEOUT) ./internal/integration -run '^TestCheckpointStore.*$$'

doctor:
	$(GO) run ./cmd/loop-harness doctor --root .

health:
	$(GO) run ./cmd/loop-harness health --root .

# platform-smoke always verifies the real settings -> Harness process/JSON
# boundary. Claude Code itself is optional on developer/CI machines; use the
# required target in an environment where the Claude CLI is installed.
platform-smoke: build
	tools/claude-hook-smoke.sh --root .

platform-smoke-required: build
	tools/claude-hook-smoke.sh --root . --require-platform

validate:
	$(GO) run ./cmd/loop-harness validate --all --root .

release-graph-validate: build
	@set -e; \
	version=release-graph-check; \
	dist=$$(mktemp -d); \
	tarball=$$dist/vibe-coding-loop-template-$$version.tar.gz; \
	bash packaging/build-release.sh $$version $$tarball >/dev/null; \
	stage=$$(mktemp -d); \
	tar -xzf $$tarball -C $$stage; \
	$$stage/vibe-coding-loop-template-$$version/.claude/bin/loop-harness release-graph validate --root $$stage/vibe-coding-loop-template-$$version; \
	rm -rf $$dist $$stage

# Default PR/main checks. Release packaging and exhaustive scenario/race
# coverage are available through ci-verify-full and the manual CI full mode.
ci-verify: fmt-check vet test-ci test-ci-race doctor validate build

# Complete local/release verification. doctor-staged already validates the
# source and installed release graphs; do not build the five-platform archive
# again via release-graph-validate in the same run.
ci-verify-full: fmt-check vet test test-race doctor doctor-staged validate build

# doctor-staged builds the release tarball, extracts it to a tmpdir, and
# installs with the packaged binary into a fresh target and verifies the
# installed layout, schemas, examples, Runtime and Manual agreement.
doctor-staged: build
	@set -e; \
	version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev); \
	dist=$$(mktemp -d); \
	tarball=$$dist/vibe-coding-loop-template-$$version.tar.gz; \
	bash packaging/build-release.sh $$version $$tarball >/dev/null; \
	stage=$$(mktemp -d); \
	tar -xzf $$tarball -C $$stage; \
	stage_root=$$stage/vibe-coding-loop-template-$$version; \
	$$stage_root/.claude/bin/loop-harness release-graph validate --root $$stage_root; \
	installed=$$stage/installed; \
	$$stage_root/.claude/bin/loop-harness install --source $$stage_root --root $$installed; \
	stage_root=$$installed; \
	$$stage_root/.claude/bin/loop-harness release-graph validate --installed --root $$stage_root; \
	$$stage_root/.claude/bin/loop-harness doctor --root $$stage_root; \
	$$stage_root/.claude/bin/loop-harness validate --all --root $$stage_root; \
	rm -rf $$dist $$stage

verify: ci-verify-full

# --- Release packaging ------------------------------------------------------
#
# Builds a distributable tarball containing only template assets (no Go source,
# no REQ-002 instance runtime, no tests). The include list is an explicit
# whitelist in packaging/include.txt.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DIST ?= dist
TARBALL := $(DIST)/vibe-coding-loop-template-$(VERSION).tar.gz

# Strip comment and blank lines from the include list.
INCLUDE := $(shell grep -v '^[[:space:]]*\#' packaging/include.txt | grep -v '^[[:space:]]*$$')

# Release staging includes its matching host executable for validation.
# Always rebuild the tarball so nested assets and Go changes cannot reuse a stale package.
release: build-all $(TARBALL)

.PHONY: force-release
force-release:

$(TARBALL): force-release $(INCLUDE) docs/guides/install.md packaging/include.txt packaging/build-release.sh
	@bash packaging/build-release.sh "$(VERSION)" "$(TARBALL)"

release-list:
	@echo "Tarball will include:"
	@echo "$(INCLUDE)" | sed 's/ /\n  /g'

release-checksum: release
	@cd $(DIST) && shasum -a 256 vibe-coding-loop-template-$(VERSION).tar.gz > sha256sums-$(VERSION).txt
	@echo "Wrote $(DIST)/sha256sums-$(VERSION).txt"

clean-release:
	@rm -rf $(DIST)
