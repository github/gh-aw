package workflow

// Shared YAML fragments for the activity-report maintenance jobs, used by both
// the main maintenance workflow and the side-repo maintenance workflow.

// activityReportReadScript is the github-script prelude that reads the generated
// activity report markdown into `reportBody`, returning early when it is unusable.
const activityReportReadScript = `            const fs = require('node:fs');
            const reportPath = './.cache/gh-aw/activity-report-logs/report.md';
            if (!fs.existsSync(reportPath)) {
              core.warning('Activity report markdown not found at ' + reportPath + '; skipping issue creation.');
              return;
            }
            let reportBody = '';
            try {
              reportBody = fs.readFileSync(reportPath, 'utf8').trim();
            } catch (error) {
              core.warning('Failed to read activity report markdown at ' + reportPath + ': ' + error.message);
              return;
            }
            if (!reportBody) {
              core.warning('Activity report markdown is empty at ' + reportPath + '; skipping issue creation.');
              return;
            }
`

// activityReportBodyScript builds the issue `body` from `repoSlug` and `reportBody`.
const activityReportBodyScript = `            const body = [
              '### Agentic workflow activity report',
              '',
              'Repository: ' + repoSlug,
              'Generated at: ' + new Date().toISOString(),
              '',
              reportBody,
            ].join('\n');
`

// activityReportSaveCacheStep returns the step that saves the activity report logs cache.
func activityReportSaveCacheStep() string {
	return `      - name: Save activity report logs cache
        if: ${{ always() }}
        uses: ` + getActionPin("actions/cache/save") + `
        with:
          path: ./.cache/gh-aw/activity-report-logs
          key: ${{ steps.activity_report_logs_cache.outputs.cache-primary-key }}
`
}
