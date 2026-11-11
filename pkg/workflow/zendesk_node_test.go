//go:build !integration

package workflow

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZendeskConnectorBundledNodeExecution(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for bundled Zendesk execution")
	}
	runtime, err := filepath.Abs("../../actions/setup/js/connector_runtime.cjs")
	require.NoError(t, err)
	read, err := zendeskPlan(t, "agent").Encode()
	require.NoError(t, err)
	write, err := zendeskPlan(t, "safe-output").Encode()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{
		"read": read, "write": write, "ticket": zendeskFixture(t, "ticket.json"),
		"note": zendeskFixture(t, "note.json"), "args": zendeskReadArgs(), "noteArgs": zendeskNoteArgs(),
	})
	require.NoError(t, err)
	const driver = `
const assert = require('node:assert/strict');
const {Readable, Writable} = require('node:stream');
const {decodePlan, executeConnector, serve} = require(process.argv[1]);
const fixture = JSON.parse(require('node:fs').readFileSync(0,'utf8'));
const plain = value => JSON.parse(JSON.stringify(value));
let calls = 0;
const envReads = [];
const credentialEnv = phase => new Proxy({}, {get: (_, key) => {
  envReads.push([phase,key]);
  if (phase === 'safe-output' && key === 'GH_AW_CONNECTOR_BINDING_RUN') return undefined;
  const expected = phase === 'agent' ? 'ZENDESK_READ_OAUTH_TOKEN' : 'ZENDESK_WRITE_OAUTH_TOKEN';
  assert.equal(key, 'GH_AW_CONNECTOR_SECRET_' + expected);
  return 'fixture-' + phase;
}});
const reader = credentialEnv('agent');
const writer = credentialEnv('safe-output');
const noSecrets = new Proxy({}, {get: (_, key) => {
  throw new Error('must not inspect environment during staging or rejection: ' + key);
}});
const noNetwork = async () => { throw new Error('must not send a request'); };
const fetchResponse = (phase, body, status = 200, text = fixture.noteArgs.text) => async (url, init) => {
  calls++;
  assert.equal(url, 'https://example.zendesk.com/api/v2/tickets/35436');
  assert.equal(init.redirect, 'manual');
  assert.equal(init.credentials, 'omit');
  assert.equal(init.headers.Authorization, 'Bearer fixture-' + phase);
  assert.equal(init.method, phase === 'agent' ? 'GET' : 'PUT');
  if (phase === 'agent') assert.equal(init.body, undefined);
  else assert.deepEqual(JSON.parse(init.body), {ticket:{comment:{body:text,public:false}}});
  return new Response(body, {status, headers:{'content-type':'application/json'}});
};
const execute = (phase, body, status = 200) => executeConnector(
  phase === 'agent' ? fixture.read : fixture.write,
  phase === 'agent' ? 'mcp' : 'safe-output',
  phase === 'agent' ? 'get-ticket' : 'add-internal-note',
  phase === 'agent' ? fixture.args : fixture.noteArgs,
  {env: phase === 'agent' ? reader : writer, fetchImpl: fetchResponse(phase,body,status)}
);
async function main() {
  for (const encoded of [fixture.read, fixture.write]) {
    const plan = decodePlan(encoded);
    assert.deepEqual([...plan.connection.resources.tickets], ['35436']);
    assert.deepEqual([...plan.connection.resources.subdomains], ['example']);
  }
  const intent = await executeConnector(fixture.write,'safe-output','add-internal-note',fixture.noteArgs,
    {staged:true,env:noSecrets,fetchImpl:noNetwork});
  assert.equal(intent.staged, true);
  assert.deepEqual(plain(intent.request.arguments), fixture.noteArgs);
  assert.equal(intent.request.digest, decodePlan(fixture.write).digest);
  assert.equal(calls, 0);
  assert.equal(envReads.length, 0);
  for (const text of ['x'.repeat(4000), 'First <&>\nSecond $(not-code)']) {
    const result = await executeConnector(fixture.write,'safe-output','add-internal-note',{...fixture.noteArgs,text},
      {staged:true,env:noSecrets,fetchImpl:noNetwork});
    assert.equal(result.request.arguments.text, text);
  }
  for (const text of ['', ' \t\r\n\f\u000b', '{{ticket.requester.email}}', '{unmatched', 'closing}', 'x'.repeat(4001), 42]) {
    await assert.rejects(executeConnector(fixture.write,'safe-output','add-internal-note',{...fixture.noteArgs,text},
      {staged:true,env:noSecrets,fetchImpl:noNetwork}));
  }
  for (const changes of [
    {subdomain:'other'}, {subdomain:'Example'}, {subdomain:'example/other'}, {ticket:'35437'},
    {ticket:35436}, {ticket:'035436'}, {ticket:'0'}, {ticket:'../35436'}, {ticket:'35436\n'},
    {ticket:'%3335436'}, {ticket:'1234567890123456'}, {page:'2'}, {url:'https://other.invalid'},
  ]) {
    for (const phase of ['agent','safe-output']) {
      await assert.rejects(executeConnector(
        phase === 'agent' ? fixture.read : fixture.write,
        phase === 'agent' ? 'mcp' : 'safe-output',
        phase === 'agent' ? 'get-ticket' : 'add-internal-note',
        {...(phase === 'agent' ? fixture.args : fixture.noteArgs),...changes},
        {env:noSecrets,fetchImpl:noNetwork}
      ));
    }
  }
  for (const field of ['public','author_id','status','html_body','uploads']) {
    await assert.rejects(executeConnector(fixture.write,'safe-output','add-internal-note',
      {...fixture.noteArgs,[field]:true},{env:noSecrets,fetchImpl:noNetwork}));
  }
  await assert.rejects(executeConnector(fixture.read,'safe-output','add-internal-note',fixture.noteArgs,
    {env:noSecrets,fetchImpl:noNetwork}));
  await assert.rejects(executeConnector(fixture.write,'mcp','get-ticket',fixture.args,
    {env:noSecrets,fetchImpl:noNetwork}));
  assert.equal(envReads.length, 0);
  assert.equal(calls, 0);
  const readResult = await execute('agent',fixture.ticket);
  assert.equal(readResult.untrusted,true);
  assert.deepEqual(plain(readResult.data), {
    'ticket-id':35436,status:'open',subject:'Actions triage fixture','updated-at':'2026-11-02T06:00:00Z'
  });
  const writeResult = await execute('safe-output',fixture.note);
  assert.equal(writeResult.untrusted,true);
  assert.deepEqual(plain(writeResult.data), {'ticket-id':35436,'audit-id':70001,'comment-id':70002,public:false});
  assert.equal(calls, 2);
  for (const phase of ['agent','safe-output']) {
    for (const status of [302,400,401,403,404,409,422,429,500]) {
      const before = calls;
      await assert.rejects(execute(phase,'{"error":"private provider diagnostic"}',status), error => {
        assert(!error.message.includes('private provider diagnostic'));
        return true;
      });
      assert.equal(calls,before+1);
    }
    const original = phase === 'agent' ? fixture.ticket : fixture.note;
    for (const size of [65536,65537]) {
      const prefix = original.trimEnd().slice(0,-1);
      const padding = size - Buffer.byteLength(prefix + ',"padding":""}');
      const body = prefix + ',"padding":"' + 'x'.repeat(padding) + '"}';
      assert.equal(Buffer.byteLength(body),size);
      const before = calls;
      if (size === 65536) {
        const result = await execute(phase,body);
        assert(!('padding' in result.data));
      } else await assert.rejects(execute(phase,body), /byte bound/);
      assert.equal(calls,before+1);
    }
    const before = calls;
    await assert.rejects(executeConnector(
      phase === 'agent' ? fixture.read : fixture.write,
      phase === 'agent' ? 'mcp' : 'safe-output',
      phase === 'agent' ? 'get-ticket' : 'add-internal-note',
      phase === 'agent' ? fixture.args : fixture.noteArgs,
      {env:phase === 'agent' ? reader : writer,fetchImpl:async () => {
        calls++;
        throw new Error('private transport diagnostic');
      }}
    ), error => {
      assert.match(error.message,/outcome may be unknown/);
      assert(!error.message.includes('private transport diagnostic'));
      return true;
    });
    assert.equal(calls,before+1);
  }
  for (const body of [
    '', '{}', 'not JSON', fixture.ticket + '{}',
    fixture.ticket.replace('"id": 35436','"id": 35437'),
    fixture.ticket.replace('"id": 35436','"id": "35436"'),
    fixture.ticket.replace('"id": 35436','"id": 35436.0'),
    fixture.ticket.replace('example.zendesk.com','other.zendesk.com'),
    fixture.ticket.replace('"subject": "Actions triage fixture"','"subject": null'),
  ]) {
    const before = calls;
    await assert.rejects(execute('agent',body));
    assert.equal(calls,before+1);
  }
  for (const body of [
    '', '{"ticket":{"id":35436}}', 'not JSON',
    fixture.note.replace('"id": 70001,',''),
    fixture.note.replace('"ticket_id": 35436','"ticket_id": 35437'),
    fixture.note.replace('"id": 35436','"id": 35436.0'),
    fixture.note.replace('"id": 70002,',''),
    fixture.note.replace('"type": "Comment"','"type": "Change"'),
    fixture.note.replace('"public": false','"public": true'),
    fixture.note.replace('"public": false','"public": "false"'),
    fixture.note.replace('"public": false,',''),
    fixture.note.replace(fixture.noteArgs.text,'Provider-normalized note.'),
    fixture.note.replace('"events": [','"events": [{"id":70000,"type":"Notification","body":"First"},'),
  ]) {
    const before = calls;
    await assert.rejects(execute('safe-output',body));
    assert.equal(calls,before+1);
  }
  for (const phase of ['agent','safe-output']) {
    const before = calls;
    await assert.rejects(executeConnector(
      phase === 'agent' ? fixture.read : fixture.write,
      phase === 'agent' ? 'mcp' : 'safe-output',
      phase === 'agent' ? 'get-ticket' : 'add-internal-note',
      phase === 'agent' ? fixture.args : fixture.noteArgs,
      {env:{},fetchImpl:async () => { calls++; throw new Error('must not fetch'); }}
    ), /credential resolution failed/);
    assert.equal(calls,before);
  }
  const messages = [
    {jsonrpc:'2.0',id:1,method:'initialize',params:{protocolVersion:'2025-03-26',capabilities:{},clientInfo:{name:'zendesk-fixture',version:'1'}}},
    {jsonrpc:'2.0',id:2,method:'tools/list'},
    {jsonrpc:'2.0',id:3,method:'tools/call',params:{name:'get_ticket',arguments:fixture.args}},
    {jsonrpc:'2.0',id:4,method:'tools/call',params:{name:'get_ticket',arguments:{...fixture.args,ticket:'35437'}}},
    {jsonrpc:'2.0',id:5,method:'tools/call',params:{name:'add_internal_note',arguments:fixture.noteArgs}},
  ];
  let transcript = '';
  const before = calls;
  await serve(fixture.read,Readable.from(messages.map(value => JSON.stringify(value)+'\n')),
    new Writable({write(chunk,encoding,callback) { transcript += chunk; callback(); }}),
    {env:reader,fetchImpl:fetchResponse('agent',fixture.ticket)});
  const replies = transcript.trim().split('\n').map(line => JSON.parse(line));
  assert.deepEqual(replies[1].result.tools.map(tool => tool.name),['get_ticket']);
  assert.equal(replies[1].result.tools[0].inputSchema.additionalProperties,false);
  assert.deepEqual(JSON.parse(replies[2].result.content[0].text).data,plain(readResult.data));
  assert.equal(replies[3].result.isError,true);
  assert.equal(replies[4].result.isError,true);
  assert.equal(calls,before+1);
  const maximumUnicode = '\u{1F4AC}'.repeat(4000);
  assert.equal(Buffer.byteLength(maximumUnicode), 16000);
  const unicodeIntent = await executeConnector(fixture.write,'safe-output','add-internal-note',
    {...fixture.noteArgs,text:maximumUnicode},{staged:true,env:noSecrets,fetchImpl:noNetwork});
  assert.equal(unicodeIntent.request.arguments.text, maximumUnicode);
  await assert.rejects(executeConnector(fixture.write,'safe-output','add-internal-note',
    {...fixture.noteArgs,text:maximumUnicode+'\u{1F4AC}'},{staged:true,env:noSecrets,fetchImpl:noNetwork}));
  const unicodeResult = await executeConnector(fixture.write,'safe-output','add-internal-note',
    {...fixture.noteArgs,text:maximumUnicode},{
      env:writer,fetchImpl:fetchResponse('safe-output',fixture.note.replace(fixture.noteArgs.text,maximumUnicode),200,maximumUnicode)
    });
  assert.deepEqual(plain(unicodeResult.data),plain(writeResult.data));
  process.stdout.write('Zendesk bundled Node execution verified\n');
}
main().catch(error => { console.error(error); process.exitCode = 1; });
`
	command := exec.CommandContext(t.Context(), node, "-e", driver, runtime)
	command.Env = []string{}
	command.Stdin = strings.NewReader(string(payload))
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.Equal(t, "Zendesk bundled Node execution verified\n", string(output))
}
