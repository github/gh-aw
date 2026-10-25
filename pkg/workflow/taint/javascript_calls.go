package taint

import (
	"path"
	"strings"
)

func (j *javascript) call(n *JavaScriptNode) jsValue {
	callee := j.eval(n.Expression)
	args := make([]jsValue, 0, len(n.Arguments))
	for _, arg := range n.Arguments {
		args = append(args, j.eval(arg))
	}
	return j.callValue(n, callee, args)
}

func (j *javascript) callValue(n *JavaScriptNode, callee jsValue, args []jsValue) jsValue { //nolint:largefunc // Built-in source, sink, and transformation semantics.
	if callee.fn != nil {
		return j.invoke(n, callee.fn, args)
	}
	if model, ok := strings.CutPrefix(callee.ref, "gh-aw-summary:"); ok {
		return j.callSummary(n, model, args)
	}
	ref := jsRefModule(callee.ref)
	all := jsUnion(args...)
	first, hasFirst := sliceAt(args, 0)
	second, _ := sliceAt(args, 1)
	switch ref {
	case "require":
		if len(args) != 1 || !first.literal {
			j.gap(n, "dynamic JavaScript module resolution is not modeled")
			return jsValue{}
		}
		return j.loadModule(n, first.text)
	case "fs.readFileSync", "fs.readFile", "fs.promises.readFile":
		if !hasFirst {
			j.gap(n, "JavaScript file read target is unresolved")
			return jsValue{}
		}
		var sources value
		if first.ref == "process.env.GITHUB_OUTPUT" {
			sources = j.files["$GITHUB_OUTPUT"]
		} else if first.literal {
			if first.text == "0" || first.text == "/dev/stdin" {
				sources = j.stdin
			} else {
				sources = j.files[path.Clean(first.text)]
			}
		} else {
			j.gap(n, "JavaScript file read target is unresolved")
		}
		v := jsValue{sources: sources, kind: jsBuffer}
		if second.literal && second.kind == jsString {
			v.kind = jsString
		}
		last, hasLast := sliceAt(args, len(args)-1)
		if ref == "fs.readFile" && len(args) > 1 && hasLast && last.fn != nil {
			j.invoke(n, last.fn, []jsValue{{}, v})
		}
		return v
	case "process.stdin.read":
		return jsValue{sources: j.stdin, kind: jsBuffer}
	case "process.stdin.on", "process.stdin.once":
		if len(args) == 2 && first.literal && second.fn != nil {
			switch first.text {
			case "data":
				j.invoke(n, second.fn, []jsValue{{sources: j.stdin, kind: jsBuffer}})
			case "end", "close":
				j.invoke(n, second.fn, nil)
			default:
				j.gap(n, "JavaScript stdin event is not modeled")
			}
		} else {
			j.gap(n, "JavaScript stdin callback is unresolved")
		}
		return jsValue{}
	case "process.stdin.setEncoding", "process.stdin.resume", "process.exit", "core.setSecret":
		return jsValue{}
	case "console.log", "console.info", "console.debug", "process.stdout.write":
		// Consume only a proved single runner command, never captured data.
		// Registration does not sanitize this value or any later use.
		if len(args) == 1 && first.mask != jsNotMask && !j.input.CaptureStdout {
			return jsValue{}
		}
		j.writeStdout(n, all.taint())
		return jsValue{}
	case "console.error", "console.warn", "process.stderr.write", "core.error", "core.warning", "core.setFailed",
		"core.info", "core.debug", "core.notice", "core.startGroup":
		j.emit(n, Log, all.taint())
		return jsValue{}
	case "core.setOutput":
		name := ""
		if first.literal && len(first.taint()) == 0 && len(args) == 2 {
			name = first.text
		}
		j.transportOutput(n, name, all.taint(), true)
		return jsValue{}
	case "core.summary.addRaw", "core.summary.addCodeBlock", "core.summary.addTable", "core.summary.addHeading":
		j.emit(n, Summary, all.taint())
		return jsValue{ref: "core.summary"}
	case "core.summary.write", "core.summary.clear", "core.endGroup":
		return jsValue{}
	case "fs.writeFileSync", "fs.appendFileSync", "fs.writeFile", "fs.appendFile", "fs.promises.writeFile", "fs.promises.appendFile":
		j.writeFile(n, args)
		return jsValue{}
	case "fetch", "http.request", "https.request", "http.get", "https.get":
		j.network(n, args)
		return jsValue{}
	case "Boolean":
		return jsValue{}
	case "JSON.parse":
		if j.catchDepth > 0 {
			j.thrown = union(j.thrown, all.taint())
		}
		all.kind = jsUnknown
		return all
	case "JSON.stringify":
		all.kind = jsString
		if len(args) <= 1 {
			all.lines = jsSingleLine
		}
		return all
	case "String":
		return jsStringCoercion(first)
	case "Number":
		return jsValue{kind: jsNumber, sources: all.taint()}
	case "Buffer.from", "Buffer.concat":
		all.kind = jsBuffer
		return all
	case "Object.values", "Object.entries":
		all.kind = jsArray
		return all
	case "Error", "TypeError", "RangeError", "SyntaxError":
		return all
	case "path.join", "path.resolve", "path.posix.join":
		parts := make([]string, 0, len(args))
		for _, arg := range args {
			if !arg.literal {
				return all
			}
			parts = append(parts, arg.text)
		}
		return jsStringLiteral(path.Join(parts...))
	case "Object.assign":
		v := jsValue{fields: make(map[string]jsValue)}
		for _, arg := range args {
			v = jsUnionPreserving(v, arg)
		}
		if target, ok := sliceAt(n.Arguments, 0); ok {
			j.bind(target, v)
		}
		return v
	}
	if n.Expression != nil && n.Expression.Kind == "PropertyAccessExpression" {
		base := j.eval(n.Expression.Expression)
		method := jsName(n.Expression.Name)
		if v, known := j.sequenceMethod(n, base, args, method); known {
			return v
		}
		switch method {
		case "then", "catch", "finally":
			if len(args) == 1 && first.fn != nil {
				return j.invoke(n, first.fn, []jsValue{base})
			}
		}
	}
	// Unknown calls may consume captured environment/stdin as well as args.
	// Neither a safe transformation nor a secret-bearing return is known.
	j.gap(n, "JavaScript call or callback data flow is not modeled")
	return jsValue{}
}

func (j *javascript) writeStdout(n *JavaScriptNode, sources value) {
	j.stdout = union(j.stdout, sources)
	if !j.input.CaptureStdout {
		j.emit(n, Log, sources)
	}
}

func (j *javascript) writeFile(n *JavaScriptNode, args []jsValue) {
	if len(args) < 2 {
		j.gap(n, "JavaScript file write arguments are unresolved")
		return
	}
	target, _ := sliceAt(args, 0)
	content, _ := sliceAt(args, 1)
	contents := content.taint()
	switch target.ref {
	case "process.env.GITHUB_STEP_SUMMARY":
		j.emit(n, Summary, contents)
	case "process.env.GITHUB_OUTPUT":
		j.transportOutput(n, "", contents, false)
	case "process.env.GITHUB_ENV":
		j.gap(n, "JavaScript persisted environment records are not modeled")
	default:
		if target.literal {
			name := path.Clean(target.text)
			j.files[name] = union(j.files[name], contents)
		} else if len(contents) > 0 {
			j.gap(n, "JavaScript file write target is unresolved")
		}
	}
}

func (j *javascript) network(n *JavaScriptNode, args []jsValue) {
	if len(args) == 0 {
		return
	}
	url, _ := sliceAt(args, 0)
	j.emit(n, Network, url.taint())
	if len(args) < 2 {
		return
	}
	options, _ := sliceAt(args, 1)
	j.emit(n, Network, options.sources)
	for key, v := range options.fields {
		if key != "headers" && key != "auth" {
			j.emit(n, Network, v.taint())
			continue
		}
		if key == "auth" {
			continue
		}
		// Unknown/opaque headers are not known authentication fields.
		j.emit(n, Network, v.sources)
		for header, credential := range v.fields {
			switch strings.ToLower(header) {
			case "authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key":
			default:
				j.emit(n, Network, credential.taint())
			}
		}
	}
}
