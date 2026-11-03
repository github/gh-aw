//go:build integration

package workflow

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

// Network-backed acceptance runs the unchanged released collector, not a local
// copy of its validator. Its exported fixture options keep all I/O unprivileged.
func TestEnclaveDiagnosticsReleasedCollector(t *testing.T) {
	if os.Getenv("GH_AW_TEST_RELEASED_COLLECTOR") != "1" {
		t.Skip("set GH_AW_TEST_RELEASED_COLLECTOR=1 to fetch and verify the immutable released collector")
	}
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get("https://raw.githubusercontent.com/github/gh-aw-firewall/3929c9a15c0f239b772affcaae6e55fec92f41c2/scripts/ci/collect-enclave-smoke-debug.cjs")
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	source, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	require.NoError(t, err)
	require.LessOrEqual(t, len(source), 1024*1024)
	require.Equal(t, "7722affc5c076d79692d4268a543ef267281424ad4b374eb8f80c24cbec08cae", fmt.Sprintf("%x", sha256.Sum256(source)))
	dir := testutil.TempDir(t, "released-collector")
	collector := filepath.Join(dir, "collector.cjs")
	require.NoError(t, os.WriteFile(collector, source, 0600))
	harness := `
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const collector = require(process.argv[1]);
const base = process.argv[2], uid = process.getuid(), gid = process.getgid();
const runId = 'a'.repeat(32), invocationId = 'b'.repeat(24);
function fixture(kind) {
  const root = path.join(base, kind);
  fs.mkdirSync(root, {mode: 0o700});
  const roots = {backend:'docker', sourceUid:uid, trustedRoot:root,
    journal:path.join(root,'journal'), debug:path.join(root,'debug')};
  for (const name of ['journal','debug','export']) fs.mkdirSync(path.join(root,name), {mode:0o700});
  const options = {privilegedUid:uid, exportRoot:path.join(root,'export'),
    exportTrustedRoot:root, candidatesForBackend:() => [roots]};
  const args = ['docker','101-1',String(uid),String(gid)];
  assert.equal(collector.main(['prepare',...args],options),0);
  const records = [{version:1,runId,state:'open'},
    {runId,invocationId,entryId:'script',state:'terminal',outcome:'executor-failure'}];
  fs.writeFileSync(path.join(roots.journal,runId+'.journal'),
    records.map(JSON.stringify).join('\n')+'\n',{mode:0o600});
  const artifacts = path.join(roots.debug,runId,invocationId);
  fs.mkdirSync(artifacts,{recursive:true,mode:0o700});
  const envelope = {version:1,runId,invocationId,entryId:'script',runtime:'docker',executorKind:'script',
    stage:'execution',failure:'workload-exit',exitCode:7,workloadStopped:true,workloadLaunched:true,
    stopProof:'verified',stages:[],
    retentionEligibility:{publicDebug:true,repositoryVisibility:'all-public-verified'},
    streams:{stdout:'captured',stderr:'captured',session:'captured'},
    streamCoverage:{stdout:{artifactSource:'workload-file',agentSession:'unavailable'},
      stderr:{artifactSource:'workload-file',agentSession:'unavailable'},copilotDiagnostics:'unavailable'},
    session:{status:'empty',bytes:0},
    backendEvidence:{dockerInspect:{status:'captured',running:false,exitCode:7,oomKilled:false},
      vmCounters:{status:'not-applicable'},network:{status:'unavailable'}}};
  if (kind==='identity') envelope.invocationId = 'c'.repeat(24);
  if (kind==='stop') envelope.workloadStopped = false;
  if (kind==='docker-stop') envelope.backendEvidence.dockerInspect.running = true;
  if (kind==='private') envelope.retentionEligibility.repositoryVisibility = 'private';
  fs.writeFileSync(path.join(artifacts,'diagnostics.json'),JSON.stringify(envelope),{mode:0o600});
  for (const name of ['stdout.log','stderr.log','enclave-session.log'])
    fs.writeFileSync(path.join(artifacts,name),name==='stderr.log'?'public workload exit 7\n':'',{mode:0o600});
  const stdout = path.join(artifacts,'stdout.log');
  if (kind==='hardlink') fs.linkSync(stdout,path.join(root,'alias'));
  if (kind==='symlink') { fs.unlinkSync(stdout); fs.symlinkSync(path.join(artifacts,'stderr.log'),stdout); }
  if (kind==='permissions') fs.chmodSync(stdout,0o644);
  const result = collector.main(['collect',...args],options);
  const upload = path.join(root,'export','101-1','upload');
  const names = fs.readdirSync(upload).sort();
  const status = fs.readFileSync(path.join(upload,'collection-status.txt'),'utf8');
  if (kind==='nonzero') {
    assert.equal(result,0);
    assert.equal(JSON.parse(fs.readFileSync(path.join(upload,'diagnostics.json'))).exitCode,7);
    assert(status.includes('Shared diagnostics envelope: validated'));
    assert(names.includes('stderr.log'));
  } else {
    assert.equal(result,1);
    assert.deepEqual(names,['collection-status.txt']);
    const expected = {identity:'diagnostics-identity',stop:'stop-proof','docker-stop':'docker-stop-proof',
      private:'retention-eligibility',hardlink:'file-links',symlink:'source-link',permissions:'file-permissions'}[kind];
    assert(status.includes('failedCheck='+expected),status);
    assert(status.includes('category='),status);
    assert(!status.includes('public workload exit'),status);
  }
}
for (const kind of ['nonzero','identity','stop','docker-stop','private','hardlink','symlink','permissions']) fixture(kind);
`
	command := exec.Command(node, "-e", harness, collector, dir)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}
