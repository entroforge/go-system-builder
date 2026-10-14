'use strict';
// This reporter is supplied by Harness from a read-only adapter mount. Its
// events come only from the controlled process. Test stdout is framed as data;
// arbitrary configuration stdout invalidates the protocol. Reviewed config/test
// code remains a trust prerequisite; this is not a malicious-JavaScript sandbox.
const fs = require('fs');
const path = require('path');
const emit = value => fs.writeSync(1, JSON.stringify(value) + '\n');
class HarnessReporter {
  onBegin(config, suite) {
    this.root = config.rootDir;
    emit({type: 'begin', tests: suite.allTests().map(test => {
      const project = test.parent.project();
      return {
        id: test.id,
        path: path.relative('/work', test.location.file).split(path.sep).join('/'),
		selection_path: path.relative(config.rootDir, test.location.file).split(path.sep).join('/'),
        project: project.name,
        browser: project.use.browserName || 'chromium',
        title_path: test.titlePath().slice(3),
        case_refs: test.annotations.filter(a => a.type === 'gsb.case').map(a => JSON.parse(a.description)),
        expected_status: test.expectedStatus,
      };
    })});
  }
  onTestBegin(test, result) { emit({type: 'test_begin', test_id: test.id, retry: result.retry}); }
  onTestEnd(test, result) {
    emit({type: 'test_end', test_id: test.id, retry: result.retry, status: result.status,
      duration_ms: result.duration, errors: result.errors.map(e => e.message || String(e))});
  }
  onStepEnd(test, result, step) {
    if (step.category !== 'pw:api') return;
    emit({type: 'api_step', test_id: test.id, retry: result.retry,
      step: {category: step.category, title: step.title,
        duration_ms: step.duration, ...(step.error ? {error: step.error.message || String(step.error)} : {})}});
  }
  onError(error) { emit({type: 'error', error: error.message || String(error)}); }
	 onStdOut(chunk) { emit({type: 'output', stream: 'stdout', text: String(chunk)}); }
	 onStdErr(chunk) { emit({type: 'output', stream: 'stderr', text: String(chunk)}); }
  onEnd(result) { emit({type: 'end', status: result.status}); }
  printsToStdio() { return true; }
}
module.exports = HarnessReporter;
