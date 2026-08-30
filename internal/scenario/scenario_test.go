package scenario_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestGenerateModuleCreatesDeterministicCurrentOutputs(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")

	first, err := scenario.GenerateModule(root, "investor-workbench")
	if err != nil {
		t.Fatalf("GenerateModule() error = %v", err)
	}
	firstCases := readScenarioFile(t, root, "cases.json")
	firstCoverage := readScenarioFile(t, root, "scenario-coverage.json")

	second, err := scenario.GenerateModule(root, "investor-workbench")
	if err != nil {
		t.Fatalf("second GenerateModule() error = %v", err)
	}
	if string(firstCases) != string(readScenarioFile(t, root, "cases.json")) {
		t.Fatal("cases.json is not byte deterministic")
	}
	if string(firstCoverage) != string(readScenarioFile(t, root, "scenario-coverage.json")) {
		t.Fatal("scenario-coverage.json is not byte deterministic")
	}
	if first.Module != "investor-workbench" || second.Module != first.Module {
		t.Fatalf("reports have wrong module: %#v %#v", first, second)
	}
	assertReportHasFields(t, first)
}

func TestValidateModuleRejectsStaleGeneratedOutput(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	path := scenarioPath(root, "cases.json")
	if err := os.WriteFile(path, append(readScenarioFile(t, root, "cases.json"), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{}); err == nil {
		t.Fatal("ValidateModule() accepted stale generated output")
	}
}

func TestScenarioValidationRejectsUnknownAndVersionFields(t *testing.T) {
	for _, field := range []string{"unknown", "version"} {
		t.Run(field, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			modelPath := filepath.Join(root, "docs/design/prototypes/investor-workbench/scenario-model.json")
			data := readScenarioFile(t, root, "scenario-model.json")
			var value map[string]any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			value[field] = "forbidden"
			writeJSON(t, modelPath, value)
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
				t.Fatalf("ValidateModule() accepted %s field", field)
			}
		})
	}
}

func TestScenarioValidationRejectsPathTraversalAndModuleMismatch(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if _, err := scenario.ValidateModule(root, "../investor-workbench", scenario.ValidateOptions{}); err == nil {
		t.Fatal("ValidateModule() accepted path traversal module")
	}
	if _, err := scenario.ValidateModule(root, "other-module", scenario.ValidateOptions{}); err == nil {
		t.Fatal("ValidateModule() accepted missing/mismatched module")
	}
	modelPath := filepath.Join(root, "docs/design/prototypes/investor-workbench/scenario-model.json")
	model := map[string]any{}
	if err := json.Unmarshal(readScenarioFile(t, root, "scenario-model.json"), &model); err != nil {
		t.Fatal(err)
	}
	model["module"] = "other-module"
	writeJSON(t, modelPath, model)
	if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{}); err == nil {
		t.Fatal("ValidateModule() accepted model module mismatch")
	}
}

func TestScenarioValidationRejectsBrokenReferencesAndOracles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{name: "duplicate ids", mutate: func(t *testing.T, root string) {
			model := loadModel(t, root)
			model["facts"].([]any)[0].(map[string]any)["partitions"].([]any)[1].(map[string]any)["id"] = "institutional"
			writeModel(t, root, model)
		}},
		{name: "unknown partition", mutate: func(t *testing.T, root string) {
			model := loadModel(t, root)
			model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)["witness"].(map[string]any)["fact-investor"] = "partition-missing"
			writeModel(t, root, model)
		}},
		{name: "missing fixture", mutate: func(t *testing.T, root string) {
			model := loadModel(t, root)
			model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)["fixture_id"] = "fixture-missing"
			writeModel(t, root, model)
		}},
		{name: "positive oracle omission", mutate: func(t *testing.T, root string) {
			model := loadModel(t, root)
			delete(model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)["oracle"].(map[string]any), "visible")
			writeModel(t, root, model)
		}},
		{name: "negative oracle omission", mutate: func(t *testing.T, root string) {
			model := loadModel(t, root)
			delete(model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[1].(map[string]any)["oracle"].(map[string]any), "rejection")
			writeModel(t, root, model)
		}},
		{name: "missing story reference", mutate: func(t *testing.T, root string) {
			model := loadModel(t, root)
			model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)["story_refs"] = []any{"S-999"}
			writeModel(t, root, model)
		}},
		{name: "missing flow reference", mutate: func(t *testing.T, root string) {
			model := loadModel(t, root)
			model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)["flow_refs"] = []any{"F-999", "PATH-999"}
			writeModel(t, root, model)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			test.mutate(t, root)
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
				t.Fatalf("ValidateModule() accepted %s", test.name)
			}
		})
	}
}

func TestScenarioValidationEnforcesProfileRatiosAndRequiredCoverage(t *testing.T) {
	for _, profile := range []string{"ordinary", "rule-dense", "critical"} {
		t.Run(profile+" ratio", func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", profile)
			model := loadModel(t, root)
			branches := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)
			if profile == "ordinary" {
				branches = branches[:1]
				model["rules"].([]any)[0].(map[string]any)["branches"] = branches
			} else {
				branches = append(branches, positiveBranch("branch-extra-"+profile, "CASE-EXTRA-"+strings.ToUpper(profile)))
				model["rules"].([]any)[0].(map[string]any)["branches"] = branches
			}
			writeModel(t, root, model)
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil || !strings.Contains(err.Error(), "coverage ratio") {
				t.Fatalf("profile %s accepted insufficient negative ratio/coverage", profile)
			}
		})
	}

	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]any{}
	if err := json.Unmarshal(readScenarioFile(t, root, "cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	cases["cases"].([]any)[1].(map[string]any)["polarity"] = "positive"
	writeJSON(t, scenarioPath(root, "cases.json"), cases)
	if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{}); err == nil {
		t.Fatal("ValidateModule() accepted branch/case polarity mismatch")
	}
}

func TestScenarioValidationAcceptsConfiguredRatios(t *testing.T) {
	for _, profile := range []string{"ordinary", "rule-dense", "critical"} {
		t.Run(profile, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", profile)
			model := loadModel(t, root)
			branches := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)
			extraNegatives := 0
			if profile == "rule-dense" {
				extraNegatives = 1
			} else if profile == "critical" {
				extraNegatives = 2
			}
			for index := 0; index < extraNegatives; index++ {
				branches = append(branches, negativeBranch(index))
			}
			model["rules"].([]any)[0].(map[string]any)["branches"] = branches
			writeModel(t, root, model)
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
				t.Fatalf("configured %s ratio rejected: %v", profile, err)
			}
			if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{}); err != nil {
				t.Fatalf("configured %s ratio failed validation: %v", profile, err)
			}
		})
	}
}

func TestValidateModuleRequireSpecsChecksCaseAndPathReferences(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{RequireSpecs: true}); err == nil {
		t.Fatal("RequireSpecs accepted missing browser spec")
	}
	specPath := filepath.Join(root, "web/e2e/investor-workbench/workbench.spec.ts")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, []byte("import { test } from '@playwright/test';\ntest('case', async () => { await page.locator('CASE-ALLOW CASE-REJECT F-001 PATH-INVESTOR'); })\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{RequireSpecs: true}); err != nil {
		t.Fatalf("RequireSpecs rejected complete spec: %v", err)
	}
	if err := os.WriteFile(specPath, []byte("test('case', () => { /* CASE-ALLOW */ })\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{RequireSpecs: true}); err == nil {
		t.Fatal("RequireSpecs accepted missing PATH reference")
	}
}

func TestRequireSpecsAcceptsOnlyStaticPlaywrightCallShapes(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		wantPass bool
	}{
		{name: "valid two argument call", source: "import { test } from '@playwright/test';\ntest('case', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n", wantPass: true},
		{name: "valid three argument call", source: "import { test } from '@playwright/test';\ntest('case', { tag: '@scenario' }, async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n", wantPass: true},
		{name: "callback used as title argument", source: "import { test } from '@playwright/test';\ntest(async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n", wantPass: false},
		{name: "dynamic title", source: "import { test } from '@playwright/test';\nconst title = 'case';\ntest(title, async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n", wantPass: false},
		{name: "callback is not last argument", source: "import { test } from '@playwright/test';\ntest('case', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); }, { tag: '@scenario' });\n", wantPass: false},
		{name: "dynamic details argument", source: "import { test } from '@playwright/test';\nconst details = { tag: '@scenario' };\ntest('case', details, async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n", wantPass: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
				t.Fatal(err)
			}
			writeSpecs(t, root, map[string]string{"workbench.spec.ts": test.source})
			_, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{RequireSpecs: true})
			if test.wantPass && err != nil {
				t.Fatalf("valid static Playwright call rejected: %v", err)
			}
			if !test.wantPass && err == nil {
				t.Fatal("invalid Playwright call shape accepted")
			}
		})
	}
}

func TestRequireSpecsRejectsImportedBindingShadowing(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "parameter shadowing canonical binding", source: "import { test } from '@playwright/test';\nfunction fake(test: any) { test('fake', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); }); }\n"},
		{name: "parameter shadowing aliased binding", source: "import { test as pwTest } from '@playwright/test';\nfunction fake(pwTest: any) { pwTest('fake', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); }); }\n"},
		{name: "local const shadowing", source: "import { test } from '@playwright/test';\nconst test = (title: string, callback: Function) => callback();\ntest('fake', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n"},
		{name: "catch binding shadowing", source: "import { test } from '@playwright/test';\ntry { throw new Error('x'); } catch (test) { test('fake', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); }); }\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
				t.Fatal(err)
			}
			writeSpecs(t, root, map[string]string{"workbench.spec.ts": test.source})
			_, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{RequireSpecs: true})
			if err == nil {
				t.Fatal("shadowed Playwright binding was accepted")
			}
		})
	}
}

func TestBrowserRequiredBranchMustReferenceAPath(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	model := loadModel(t, root)
	branch := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)
	branch["flow_refs"] = []any{"F-001"}
	writeModel(t, root, model)
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
		t.Fatal("browser-required branch without PATH was accepted")
	}
}

func TestRequireSpecsUsesExactIDsInsideActualPlaywrightTestBlocks(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		wantPass bool
	}{
		{name: "prefix and suffix bypass", files: map[string]string{
			"workbench.spec.ts": "import { test } from '@playwright/test';\ntest('case', async () => { await page.locator('CASE-ALLOWX CASE-REJECTX PATH-INVESTOR-ALT'); });\n",
		}, wantPass: false},
		{name: "comment bypass", files: map[string]string{
			"workbench.spec.ts": "import { test } from '@playwright/test';\ntest('case', async () => { /* CASE-ALLOW CASE-REJECT PATH-INVESTOR */ });\n",
		}, wantPass: false},
		{name: "fake test inside ordinary string", files: map[string]string{
			"workbench.spec.ts": "const fake = \"test('fake', async () => { 'CASE-ALLOW CASE-REJECT PATH-INVESTOR' })\";\n",
		}, wantPass: false},
		{name: "local fake test without import", files: map[string]string{
			"workbench.spec.ts": "const test = (title, callback) => callback();\ntest('case', async () => { await fake('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n",
		}, wantPass: false},
		{name: "unrelated playwright import with local fake test", files: map[string]string{
			"workbench.spec.ts": "import { expect } from '@playwright/test';\nconst test = (title, callback) => callback();\ntest('case', async () => { await fake('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n",
		}, wantPass: false},
		{name: "title-only IDs", files: map[string]string{
			"workbench.spec.ts": "import { test } from '@playwright/test';\ntest('CASE-ALLOW CASE-REJECT PATH-INVESTOR', async () => { await page.goto('/'); });\n",
		}, wantPass: false},
		{name: "separate test blocks do not bind", files: map[string]string{
			"workbench.spec.ts": "import { test } from '@playwright/test';\n" +
				"test('case', async () => { await page.locator('CASE-ALLOW'); });\n" +
				"test('path', async () => { await page.locator('CASE-REJECT PATH-INVESTOR'); });\n",
		}, wantPass: false},
		{name: "tsx is not a spec", files: map[string]string{
			"workbench.spec.tsx": "import { test } from '@playwright/test';\ntest('case', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n",
		}, wantPass: false},
		{name: "imported Playwright callback body", files: map[string]string{
			"workbench.spec.ts": "import { test } from '@playwright/test';\ntest('case', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n",
		}, wantPass: true},
		{name: "safely parsed Playwright alias", files: map[string]string{
			"workbench.spec.ts": "import { test as pwTest } from \"@playwright/test\";\npwTest('case', async () => { await page.locator('CASE-ALLOW CASE-REJECT PATH-INVESTOR'); });\n",
		}, wantPass: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
				t.Fatal(err)
			}
			writeSpecs(t, root, test.files)
			_, err := scenario.ValidateModule(root, "investor-workbench", scenario.ValidateOptions{RequireSpecs: true})
			if test.wantPass && err != nil {
				t.Fatalf("valid Playwright test rejected: %v", err)
			}
			if !test.wantPass && err == nil {
				t.Fatal("invalid or bypassed spec accepted")
			}
		})
	}
}

func TestScenarioReferencesRequireExactMarkdownHeadings(t *testing.T) {
	tests := []struct {
		name    string
		stories string
		flows   string
	}{
		{name: "story suffix", stories: "# Stories\n\n## S-0010\n\nS-001\n", flows: "# Flows\n\n## F-001\n\n### PATH-INVESTOR\n"},
		{name: "path suffix", stories: "# Stories\n\n## S-001\n", flows: "# Flows\n\n## F-001\n\n### PATH-INVESTOR-ALT\n\nPATH-INVESTOR\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			dir := filepath.Join(root, "docs/design/prototypes/investor-workbench")
			if err := os.WriteFile(filepath.Join(dir, "stories.md"), []byte(test.stories), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "flows.md"), []byte(test.flows), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
				t.Fatal("non-heading or suffixed reference was accepted")
			}
		})
	}
}

func TestScenarioRejectsSymlinkModuleAndSourceOutputFiles(t *testing.T) {
	t.Run("module directory", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		external := filepath.Join(t.TempDir(), "investor-workbench")
		if err := os.Rename(filepath.Join(root, "docs/design/prototypes/investor-workbench"), external); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(root, "docs/design/prototypes/investor-workbench")); err != nil {
			t.Fatal(err)
		}
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
			t.Fatal("symlink module directory was accepted")
		}
	})

	t.Run("source file", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		external := filepath.Join(t.TempDir(), "scenario-model.json")
		if err := os.WriteFile(external, readScenarioFile(t, root, "scenario-model.json"), 0o644); err != nil {
			t.Fatal(err)
		}
		modelPath := scenarioPath(root, "scenario-model.json")
		if err := os.Remove(modelPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, modelPath); err != nil {
			t.Fatal(err)
		}
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
			t.Fatal("symlink source file was accepted")
		}
	})

	t.Run("output file", func(t *testing.T) {
		root := newScenarioRoot(t, "investor-workbench", "ordinary")
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
			t.Fatal(err)
		}
		external := filepath.Join(t.TempDir(), "cases.json")
		if err := os.WriteFile(external, readScenarioFile(t, root, "cases.json"), 0o644); err != nil {
			t.Fatal(err)
		}
		casesPath := scenarioPath(root, "cases.json")
		if err := os.Remove(casesPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, casesPath); err != nil {
			t.Fatal(err)
		}
		if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
			t.Fatal("symlink output file was accepted")
		}
	})
}

func TestScenarioOracleSchemaParity(t *testing.T) {
	tests := []struct {
		polarity string
		missing  string
	}{
		{polarity: "positive", missing: "visible"},
		{polarity: "negative", missing: "visible"},
		{polarity: "negative", missing: "terminal_state"},
		{polarity: "negative", missing: "persisted_effects"},
		{polarity: "negative", missing: "forbidden_side_effects"},
		{polarity: "negative", missing: "rejection"},
		{polarity: "negative", missing: "expected_state"},
		{polarity: "negative", missing: "recovery"},
	}
	for _, test := range tests {
		t.Run(test.polarity+" missing "+test.missing, func(t *testing.T) {
			model := validModel("investor-workbench", "ordinary")
			branches := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)
			branch := branches[0].(map[string]any)
			if test.polarity == "negative" {
				branch = branches[1].(map[string]any)
			}
			oracle := branch["oracle"].(map[string]any)
			delete(oracle, test.missing)
			modelBytes, err := json.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("scenario-model.schema.json", modelBytes); err == nil {
				t.Fatal("scenario-model schema accepted incomplete oracle")
			}
			cases := map[string]any{"module": "investor-workbench", "cases": []any{map[string]any{
				"id": "CASE-TEST", "rule_id": "RULE-TEST", "branch_id": "BRANCH-TEST", "title": "test",
				"polarity": test.polarity, "required": true, "witness": map[string]any{"fact-investor": "institutional"},
				"oracle": oracle, "fixture_id": "fixture-investor", "story_refs": []any{"S-001"},
				"flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true,
			}}}
			casesBytes, err := json.Marshal(cases)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("scenario-cases.schema.json", casesBytes); err == nil {
				t.Fatal("scenario-cases schema accepted incomplete oracle")
			}
		})
	}
}

func TestScenarioSchemasRequireNAARecoveryEvidenceWithEngineWhitespaceSemantics(t *testing.T) {
	variants := []string{"N/A", " n/a ", "\tN/A\n", "n/A"}
	for _, variant := range variants {
		t.Run(strings.ReplaceAll(strings.TrimSpace(variant), "/", "-"), func(t *testing.T) {
			model := validModel("investor-workbench", "ordinary")
			negative := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[1].(map[string]any)
			oracle := negative["oracle"].(map[string]any)
			oracle["recovery"] = variant
			oracle["recovery_source_refs"] = []any{"REQ-INV-001"}
			oracle["recovery_reason"] = "No recovery path is defined by the source rule."
			modelBytes, err := json.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("scenario-model.schema.json", modelBytes); err != nil {
				t.Fatalf("model schema rejected N/A variant %q: %v", variant, err)
			}
			cases := map[string]any{"module": "investor-workbench", "cases": []any{map[string]any{
				"id": "CASE-REJECT", "rule_id": "RULE-TEST", "branch_id": "BRANCH-TEST", "title": "test",
				"polarity": "negative", "required": true, "witness": map[string]any{"fact-investor": "individual"},
				"oracle": oracle, "fixture_id": "fixture-investor", "story_refs": []any{"S-001"},
				"flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true,
			}}}
			casesBytes, err := json.Marshal(cases)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("scenario-cases.schema.json", casesBytes); err != nil {
				t.Fatalf("cases schema rejected N/A variant %q: %v", variant, err)
			}
		})
	}

	for _, missing := range []string{"recovery_source_refs", "recovery_reason"} {
		t.Run("missing "+missing, func(t *testing.T) {
			model := validModel("investor-workbench", "ordinary")
			negative := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[1].(map[string]any)
			oracle := negative["oracle"].(map[string]any)
			oracle["recovery"] = " n/A "
			delete(oracle, missing)
			modelBytes, err := json.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("scenario-model.schema.json", modelBytes); err == nil {
				t.Fatalf("model schema accepted N/A without %s", missing)
			}
			cases := map[string]any{"module": "investor-workbench", "cases": []any{map[string]any{
				"id": "CASE-REJECT", "rule_id": "RULE-TEST", "branch_id": "BRANCH-TEST", "title": "test",
				"polarity": "negative", "required": true, "witness": map[string]any{"fact-investor": "individual"},
				"oracle": oracle, "fixture_id": "fixture-investor", "story_refs": []any{"S-001"},
				"flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true,
			}}}
			casesBytes, err := json.Marshal(cases)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("scenario-cases.schema.json", casesBytes); err == nil {
				t.Fatalf("cases schema accepted N/A without %s", missing)
			}
		})
	}
}

func TestScenarioSchemasDeclareObjectTypeForObjectKeywords(t *testing.T) {
	for _, name := range []string{
		"scenario-model.schema.json",
		"scenario-cases.schema.json",
	} {
		t.Run(name, func(t *testing.T) {
			data, err := schema.ReadAsset(name)
			if err != nil {
				t.Fatal(err)
			}
			var document any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			assertExplicitObjectTypes(t, name, document)
		})
	}
}

func assertExplicitObjectTypes(t *testing.T, path string, value any) {
	t.Helper()
	assertSchemaNodeObjectTypes(t, path, value, true)
}

func assertSchemaNodeObjectTypes(t *testing.T, path string, value any, isSchemaNode bool) {
	t.Helper()
	switch current := value.(type) {
	case map[string]any:
		if isSchemaNode {
			if _, hasProperties := current["properties"]; hasProperties && current["type"] != "object" {
				t.Errorf("%s uses properties without type object", path)
			}
			if requiredValue, hasRequired := current["required"]; hasRequired {
				if current["type"] != "object" {
					t.Errorf("%s uses required without type object", path)
				}
				properties, _ := current["properties"].(map[string]any)
				for _, required := range requiredValue.([]any) {
					if _, defined := properties[required.(string)]; !defined {
						t.Errorf("%s requires locally undefined property %q", path, required)
					}
				}
			}
		}
		for key, child := range current {
			container := key == "properties" || key == "$defs" || key == "patternProperties" || key == "dependentSchemas"
			assertSchemaNodeObjectTypes(t, path+"/"+key, child, !container)
		}
	case []any:
		for index, child := range current {
			assertSchemaNodeObjectTypes(t, path+"/"+strconv.Itoa(index), child, true)
		}
	}
}

func TestNegativeOracleEngineRequiresCommonAndRejectionFields(t *testing.T) {
	required := []string{
		"visible", "terminal_state", "persisted_effects", "forbidden_side_effects",
		"rejection", "expected_state", "recovery",
	}
	for _, field := range required {
		t.Run(field, func(t *testing.T) {
			root := newScenarioRoot(t, "investor-workbench", "ordinary")
			model := loadModel(t, root)
			negative := model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[1].(map[string]any)
			delete(negative["oracle"].(map[string]any), field)
			writeModel(t, root, model)
			if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
				t.Fatalf("negative oracle without %s was accepted", field)
			}
		})
	}
}

func TestValidateAllIgnoresPrototypeRootTemplatesAndReturnsEmptyWithoutModules(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs/design/prototypes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/design/prototypes/README.md"), []byte("template"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs/design/prototypes/templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs/design/prototypes/templates/scenario-model.json"), []byte("not-a-module"), 0o644); err != nil {
		t.Fatal(err)
	}
	reports, err := scenario.ValidateAll(root, scenario.ValidateOptions{})
	if err != nil {
		t.Fatalf("ValidateAll() error = %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("ValidateAll() reports = %#v, want empty", reports)
	}
}

func TestGenerateFailurePreservesPriorOutputs(t *testing.T) {
	root := newScenarioRoot(t, "investor-workbench", "ordinary")
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err != nil {
		t.Fatal(err)
	}
	beforeCases := readScenarioFile(t, root, "cases.json")
	beforeCoverage := readScenarioFile(t, root, "scenario-coverage.json")
	model := loadModel(t, root)
	model["rules"].([]any)[0].(map[string]any)["branches"].([]any)[0].(map[string]any)["fixture_id"] = "fixture-missing"
	writeModel(t, root, model)
	if _, err := scenario.GenerateModule(root, "investor-workbench"); err == nil {
		t.Fatal("GenerateModule() accepted invalid source")
	}
	if got := readScenarioFile(t, root, "cases.json"); string(got) != string(beforeCases) {
		t.Fatal("failed generation changed cases.json")
	}
	if got := readScenarioFile(t, root, "scenario-coverage.json"); string(got) != string(beforeCoverage) {
		t.Fatal("failed generation changed scenario-coverage.json")
	}
}

func newScenarioRoot(t *testing.T, module, profile string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "docs/design/prototypes", module)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "scenario-model.json"), validModel(module, profile))
	writeJSON(t, filepath.Join(dir, "fixture-contract.json"), validFixtures(module))
	writeJSON(t, filepath.Join(dir, "cross-matrix.json"), validCrossMatrix(module))
	if err := os.WriteFile(filepath.Join(dir, "stories.md"), []byte("# Stories\n\n## S-001\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flows.md"), []byte("# Flows\n\n## F-001\n\n### PATH-INVESTOR\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func validModel(module, profile string) map[string]any {
	return map[string]any{
		"module":           module,
		"coverage_profile": profile,
		"facts": []any{
			map[string]any{"id": "fact-investor", "partitions": []any{
				map[string]any{"id": "institutional", "value": "institutional"},
				map[string]any{"id": "individual", "value": "individual"},
			}},
		},
		"rules": []any{map[string]any{
			"id":          "rule-investor-type",
			"source_refs": []any{"REQ-INV-001"},
			"risk":        "ordinary",
			"branches": []any{
				map[string]any{
					"id":       "branch-allow",
					"case_id":  "CASE-ALLOW",
					"title":    "Institutional investor completes filing",
					"polarity": "positive",
					"required": true,
					"witness":  map[string]any{"fact-investor": "institutional"},
					"oracle": map[string]any{
						"visible":                []any{"filing-form"},
						"terminal_state":         "submitted",
						"persisted_effects":      []any{"filing-created"},
						"forbidden_side_effects": []any{"duplicate-filing"},
					},
					"fixture_id":       "fixture-investor",
					"story_refs":       []any{"S-001"},
					"flow_refs":        []any{"F-001", "PATH-INVESTOR"},
					"browser_required": true,
				},
				map[string]any{
					"id":       "branch-reject",
					"case_id":  "CASE-REJECT",
					"title":    "Individual investor is rejected",
					"polarity": "negative",
					"required": true,
					"witness":  map[string]any{"fact-investor": "individual"},
					"oracle": map[string]any{
						"visible":                []any{"validation-error"},
						"terminal_state":         "draft",
						"persisted_effects":      []any{"draft-retained"},
						"rejection":              "institutional-only",
						"expected_state":         "draft",
						"forbidden_side_effects": []any{"filing-created"},
						"recovery":               "select-institutional",
					},
					"fixture_id":       "fixture-investor",
					"story_refs":       []any{"S-001"},
					"flow_refs":        []any{"F-001", "PATH-INVESTOR"},
					"browser_required": true,
				},
			},
		}},
	}
}

func validFixtures(module string) map[string]any {
	return map[string]any{
		"module": module,
		"fixtures": []any{map[string]any{
			"id":        "fixture-investor",
			"persona":   "investor-operator",
			"synthetic": true,
			"setup":     []any{"create-synthetic-investor"},
			"cleanup":   []any{"delete-synthetic-investor"},
		}},
	}
}

func validCrossMatrix(module string) map[string]any {
	return map[string]any{
		"module": module,
		"entries": []any{
			map[string]any{"fact": "fact-investor", "req_ref": "REQ-INV-001", "story": "S-001", "branch": "branch-allow"},
		},
	}
}

func positiveBranch(branchID, caseID string) map[string]any {
	return map[string]any{
		"id": branchID, "case_id": caseID, "title": "Additional allowed filing",
		"polarity": "positive", "required": true,
		"witness": map[string]any{"fact-investor": "institutional"},
		"oracle": map[string]any{
			"visible": []any{"filing-form"}, "terminal_state": "submitted",
			"persisted_effects":      []any{"filing-created"},
			"forbidden_side_effects": []any{"duplicate-filing"},
		},
		"fixture_id": "fixture-investor", "story_refs": []any{"S-001"},
		"flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true,
	}
}

func negativeBranch(index int) map[string]any {
	suffix := strconv.Itoa(index)
	return map[string]any{
		"id":      "branch-extra-negative-" + suffix,
		"case_id": "CASE-EXTRA-NEGATIVE-" + suffix,
		"title":   "Additional rejected filing", "polarity": "negative", "required": true,
		"witness": map[string]any{"fact-investor": "individual"},
		"oracle": map[string]any{
			"visible": []any{"validation-error"}, "terminal_state": "draft",
			"persisted_effects": []any{"draft-retained"},
			"rejection":         "institutional-only", "expected_state": "draft",
			"forbidden_side_effects": []any{"filing-created"}, "recovery": "select-institutional",
		},
		"fixture_id": "fixture-investor", "story_refs": []any{"S-001"},
		"flow_refs": []any{"F-001", "PATH-INVESTOR"}, "browser_required": true,
	}
}

func loadModel(t *testing.T, root string) map[string]any {
	t.Helper()
	var model map[string]any
	if err := json.Unmarshal(readScenarioFile(t, root, "scenario-model.json"), &model); err != nil {
		t.Fatal(err)
	}
	return model
}

func writeModel(t *testing.T, root string, model map[string]any) {
	t.Helper()
	writeJSON(t, filepath.Join(root, "docs/design/prototypes/investor-workbench/scenario-model.json"), model)
}

func writeSpecs(t *testing.T, root string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "web/e2e/investor-workbench")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func scenarioPath(root, name string) string {
	return filepath.Join(root, "docs/design/prototypes/investor-workbench", name)
}

func readScenarioFile(t *testing.T, root, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(scenarioPath(root, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func assertReportHasFields(t *testing.T, report scenario.Report) {
	t.Helper()
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, field := range []string{"module", "counts", "coverage", "ratio", "browser", "fingerprint"} {
		if !strings.Contains(text, field) {
			t.Fatalf("report JSON lacks %q: %s", field, text)
		}
	}
}
