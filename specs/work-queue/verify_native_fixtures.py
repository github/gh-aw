"""Read the contract owner's actual fixtures; never rewrite or derive their answers."""

import contextlib
import hashlib
import json
import math


def canonical_cases():
    # Independent literal expectations for normative integer/Unicode edges.
    return [
        {"name": "safe-integer-boundary", "input": '{"z":9007199254740991,"a":"9007199254740993"}',
         "expected": '{"a":"9007199254740993","z":9007199254740991}'},
        {"name": "UTF8-not-UTF16-key-order", "input": '{"𐀀":1,"":2}',
         "expected": '{"":2,"𐀀":1}'},
        {"name": "unescaped-HTML-and-line-separators", "input": '{"a":"<>&\\u2028\\u2029"}',
         "expected": '{"a":"<>&\u2028\u2029"}'},
        {"name": "duplicate-escaped-equivalent-key", "input": '{"a":1,"\\u0061":2}', "reject": True},
        {"name": "unsafe-integer-number", "input": '{"a":9007199254740992}', "reject": True},
        {"name": "negative-zero", "input": '{"a":-0}', "reject": True},
        {"name": "fraction", "input": '{"a":1.5}', "reject": True},
        {"name": "exponent", "input": '{"a":1e0}', "reject": True},
        {"name": "lone-surrogate", "input": '{"a":"\\ud800"}', "reject": True, "error_code": "invalid_unicode"},
    ]


def shared_canonical_cases(codec, extra):
    cases = [dict(case) for case in codec["valid"]]
    for index, case in enumerate(codec["invalid"], 1):
        fields = {"input": case} if isinstance(case, str) else dict(case)
        cases.append({"name": f"shared-invalid-{index}", **fields, "reject": True})
    cases.extend({"name": f"shared-error-code/{case['name']}", "input": case["input"],
                  "reject": True, "error_code": case["code"]}
                 for case in codec.get("error_codes", []))
    for case in extra["codec"]:
        fields = {"reject": True} if case.get("invalid") else {"expected": case["canonical"]}
        if "error_code" in case:
            fields["error_code"] = case["error_code"]
        cases.append({"name": f"extra-shared/{case['name']}", "input": case["input"], **fields})
    return cases


def verify_canonical_output(case, output):
    if case.get("reject"):
        error = output.get("error")
        assert isinstance(error, str) and error, "accepted invalid canonical input or missing error diagnostic"
        if "error_code" in case:
            actual = error.split(":", 1)[0]
            assert actual == case["error_code"], f"expected {case['error_code']}, got {output}"
    else:
        assert output == {"canonical": case["expected"]}, f"expected exact canonical bytes, got {output}"


def reason_ledger(reason, api):
    ledger = api.Ledger()
    ledger.append("control", [{"kind": "Control", "control": "admission_paused",
                               "value": False, "reason": reason}])
    return ledger


def identity_ledger(value, decimal, api):
    ledger = api.Ledger()
    commit = ledger.commits[0]
    if decimal:
        commit["actor"].update(workflow=".github/workflows/administrator.lock.yml",
                               run_id=value, run_attempt=1)
        commit["request"]["fingerprint"] = api.digest({
            "actor": commit["actor"], "kind": "policy", "parameters": commit["request"]["parameters"],
        })
    else:
        commit["id"] = value
    return ledger


def principal_policy_cases(api):
    values = [
        ("numeric", "1001", True), ("maximum-digits", "9" * 256, True),
        ("login", "operator", False), ("zero", "0", False),
        ("leading-zero", "001", False), ("negative", "-1", False),
        ("fraction", "1.0", False), ("exponent", "1e3", False),
    ]
    cases = []
    for name, principal, valid in values:
        for scope in ("profile", "producer"):
            policy = api.default_policy()
            if scope == "profile":
                policy["pools"]["default"]["profiles"]["default"]["principal"] = principal
            else:
                policy["producers"] = {principal: policy["producers"]["42"]}
            cases.append({"name": f"principal-{scope}-{name}", "policy": policy,
                          "valid": valid, "error_code": "policy_invalid"})
    return cases


def structural_wire_cases(source, api):
    cases = []
    claim = next(commit for commit in source["commits"]
                 if any(operation["kind"] == "Claim" for operation in commit["operations"]))
    for name, references, valid in (
        ("empty", [], True),
        ("maximum-unique", [f"o_{index}" for index in range(64)], True),
        ("over-maximum", [f"o_{index}" for index in range(65)], False),
        ("duplicate", ["o_1", "o_1"], False),
    ):
        record = json.loads(json.dumps(claim))
        operation = next(operation for operation in record["operations"] if operation["kind"] == "Claim")
        operation["observations"] = references
        cases.append({"name": f"wire-claim-observations-{name}", "record": record, "valid": valid})
    observation = next(commit for commit in source["commits"]
                       if any(operation["kind"] == "Observation" for operation in commit["operations"]))
    for scope in ("actor-principal", "observation-resource-id"):
        for name, value, valid in (
            ("positive", "1", True), ("maximum-digits", "9" * 256, True),
            ("over-digits", "9" * 257, False), ("zero", "0", False),
            ("leading-zero", "01", False), ("unicode-digits", "\u0661", False),
        ):
            record = json.loads(json.dumps(source["commits"][0] if scope == "actor-principal" else observation))
            if scope == "actor-principal":
                record["actor"]["principal"] = value
            else:
                operation = next(operation for operation in record["operations"] if operation["kind"] == "Observation")
                operation["resource"]["resource_id"] = value
                record["request"]["parameters"]["operations"] = record["operations"]
            record["request"]["fingerprint"] = api.digest({
                "actor": record["actor"], "kind": record["request"]["kind"],
                "parameters": record["request"]["parameters"],
            })
            cases.append({"name": f"wire-{scope}-{name}", "record": record, "valid": valid})
    return cases


def delivery_deadline_cases(source, deadlines, api):
    completion_index = next(index for index, commit in enumerate(source["commits"])
                            if any(operation["kind"] == "Completion" for operation in commit["operations"]))
    prefix = json.loads(json.dumps(source["commits"][:completion_index + 1]))
    for record in prefix:
        record["at"] = min(record["at"], deadlines["completion_at"])
    prefix[-1]["at"] = deadlines["completion_at"]
    completed = next(operation for operation in prefix[-1]["operations"] if operation["kind"] == "Completion")
    reconciliation = prefix[0]["operations"][0]["policy"]["pools"]["default"]["reconciliation"]
    assert reconciliation == {"max_attempts": deadlines["max_attempts"], "deadline_ms": deadlines["deadline_ms"]}
    template = next(commit for commit in source["commits"]
                    if any(operation["kind"] == "DeliveryFailure" for operation in commit["operations"]))
    cases = []
    for case in deadlines["cases"]:
        record = json.loads(json.dumps(template))
        operation = next(operation for operation in record["operations"] if operation["kind"] == "DeliveryFailure")
        operation.update(work_id=completed["work_id"], claim_id=completed["claim_id"],
                         completion_id=prefix[-1]["id"], disposition=case["disposition"])
        evidence = operation["evidence"]
        evidence.update(checked_at=case["at"])
        evidence.pop("attempts", None)
        if case["attempts"]:
            evidence["attempts"] = case["attempts"]
        for field in ("effects", "receipt"):
            evidence.pop(field, None)
            if field in case:
                evidence[field] = case[field]
        record.update(id=f"deadline-{case['id']}", previous=prefix[-1]["id"], at=case["at"],
                      operations=[operation])
        record["request"].update(id=record["id"], parameters={"operations": record["operations"]})
        record["request"]["fingerprint"] = api.digest({
            "actor": record["actor"], "kind": record["request"]["kind"],
            "parameters": record["request"]["parameters"],
        })
        cases.append({"name": case["id"], "commits": [*prefix, record], "expected": case["expected"],
                      "at": case["at"], "work_id": completed["work_id"], "disposition": case["disposition"]})
    return cases


def shared_selection_case(case, api):
    return {
        "name": f"extra-shared/{case['name']}", "mode": case["mode"],
        "class_weights": api.default_policy()["class_weights"], "accounting_weights": case["accounting_weights"],
        "nodes": [{"key": work["name"], "priority": work["priority"], "account": work["fairness_key"],
                   "profile": "default", "enqueued": work["enqueued"]} for work in case["works"]],
        "max_claims": len(case["expected_names"]), "max_dispatches": 256,
        "profile_max": 16, "share_keys": False, "expected": case["expected_names"],
        "reason": "claim_budget_reached",
    }


def byte_packing_case():
    return {
        "name": "byte-overflow-opens-group-and-earliest-fit-is-reused", "mode": "weighted-priority",
        "class_weights": [8, 4, 2, 1, 1], "accounting_weights": {"": 1},
        "nodes": [{"key": key, "priority": 3, "account": "", "profile": "default", "enqueued": 0,
                   "payload_bytes": size} for key, size in (("a", 9000), ("b", 9000), ("c", 1000))],
        "max_claims": 3, "max_dispatches": 2, "max_bytes": 18000,
        "profile_max": 2, "share_keys": False, "expected": ["a", "b", "c"],
        "reason": "claim_budget_reached",
    }


def selection_ledger(case, api):
    policy = api.default_policy()
    policy["mode"], policy["class_weights"] = case["mode"], case["class_weights"]
    policy["accounting_weights"] = case["accounting_weights"]
    policy["producers"]["42"]["fairness_keys"] = list(case["accounting_weights"])
    profiles = {}
    for item in case["nodes"]:
        profile = dict(policy["pools"]["default"]["profiles"]["default"])
        profile.update(max_claims=case["profile_max"], share_keys=case["share_keys"])
        profiles[item["profile"]] = profile
    pool = policy["pools"]["default"]
    pool["profiles"], pool["default_profile"] = profiles, next(iter(profiles))
    ledger = api.Ledger(policy)
    nodes = []
    for item in case["nodes"]:
        node = ledger.work(item["key"], payload_size=0)
        node.update(priority=item["priority"], fairness_key=item["account"], worker_profile=item["profile"],
                    enqueued=item["enqueued"], payload={"task": item["key"]})
        if item.get("payload_bytes"):
            node["payload"]["padding"] = "x" * item["payload_bytes"]
        nodes.append(node)
    ledger.submit(nodes)
    return ledger, {node["node_key"]: node for node in nodes}


def packing_oracle(case, nodes, api, request_id, commit_id):
    # Selection order comes ONLY from the shared independently expected answer.
    # This oracle supplies deterministic identity/group encoding, not selection.
    prefix = hashlib.sha256(request_id.encode()).hexdigest()
    assignments, operations = [], []
    for ordinal, key in enumerate(case["expected"], 1):
        work = nodes[key]
        group = None
        claim_id = f"c_{prefix}_{ordinal}"
        for item in assignments:
            if (item["worker_profile"] != work["worker_profile"] or len(item["claims"]) >= case["profile_max"] or
                    not case["share_keys"] and nodes[item["claims"][0]["work"]["task"]]["fairness_key"] != work["fairness_key"]):
                continue
            trial_claim = {"handle": f"h{len(item['claims'])+1}", "claim_id": claim_id, "work_id": work["work_id"],
                           "work": work["payload"], "result_refs": []}
            if len(api.canonical({**item, "claims": [*item["claims"], trial_claim]}).encode()) <= case.get("max_bytes", 49152):
                group = item
                break
        if group is None:
            assert len(assignments) < case["max_dispatches"]
            group = {"version": 3, "dispatch_id": f"d_{prefix}_{len(assignments)+1}", "request_id": request_id,
                     "commit_id": commit_id, "policy_epoch": "epoch", "pool": "default",
                     "worker_profile": work["worker_profile"], "claims": []}
            assignments.append(group)
        handle = f"h{len(group['claims'])+1}"
        group["claims"].append({"handle": handle, "claim_id": claim_id, "work_id": work["work_id"],
                                "work": work["payload"], "result_refs": []})
        assert len(api.canonical(group).encode()) <= case.get("max_bytes", 49152)
        operations.append({"kind": "Claim", "work_id": work["work_id"], "claim_id": claim_id,
                           "dispatch_id": group["dispatch_id"], "handle": handle, "observations": []})
    return operations, assignments


def verify_prefix(output, expected):
    if "error" in output:
        raise AssertionError(output["error"])
    state = output["projection"]
    assert state["tip"] == expected["tip"]
    assert state["policy_epoch"] == expected["policy_epoch"]
    assert state["stats"]["claims"] == expected["claims"]
    assert state["stats"]["dispatches"] == expected["native_reservations"]
    actual = {work["node_key"]: {"state": work["state"], "barrier": work["barrier"]} for work in state["works"].values()}
    assert actual == expected["works"], f"independent ownership/barriers: {actual} != {expected['works']}"
    selected = output["selection"]
    node = state["works"][selected["work_id"]]["node_key"] if selected.get("work_id") else ""
    assert node == expected["next_node"], f"independent winner: {node} != {expected['next_node']}"
    assert selected["reason"] == expected["reason"]
    clock = state["clocks"].get("default", {"classes": {"v": "0"}, "keys": {}})
    assert clock["classes"]["v"] == expected["class_v"]
    assert clock["keys"].get("3", {"v": "0"})["v"] == expected["key_v"]


def conformance(fixtures, commands, env, evidence, api):
    paths = {file: fixtures / file
             for file in ("canonical.json", "canonical-prefix.json", "selection.json", "contract.json",
                          "reason-validation.json", "identity-validation.json", "delivery-deadlines.json")}
    extra_path = "actions/setup/js/work_queue_conformance_fixtures.json"
    paths[extra_path] = api.ROOT / extra_path
    raw = {file: path.read_bytes() for file, path in paths.items()}
    hashes = {file: hashlib.sha256(data).hexdigest() for file, data in raw.items()}
    source = json.loads(raw["canonical-prefix.json"])
    selections = json.loads(raw["selection.json"])
    contract = json.loads(raw["contract.json"])
    codec = json.loads(raw["canonical.json"])
    reasons = json.loads(raw["reason-validation.json"])
    identities = json.loads(raw["identity-validation.json"])
    deadlines = json.loads(raw["delivery-deadlines.json"])
    extra = json.loads(raw[extra_path])
    assert contract["version"] == 3
    assert reasons["version"] == 3
    assert reasons["pattern"] == "^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"
    assert reasons["cases"]
    assert identities["version"] == 3
    assert identities["identity_max_utf8_bytes"] == identities["decimal_max_digits"] == 256
    assert extra["version"] == 3
    assert deadlines["version"] == 3
    assert deadlines["cases"]
    assert len(source["prefixes"]) == len(source["commits"]) > 0
    assert source["canonical"] == api.ordered_ledger(api.ledger_text(source["commits"]))
    rows, failures = [], []
    with contextlib.ExitStack() as stack:
        engines = {name: stack.enter_context(api.Native(command, env)) for name, command in commands.items()}
        for case in shared_canonical_cases(codec, extra) + canonical_cases():
            outputs = {name: engine.call({"action": "canonical", "data": case["input"]}) for name, engine in engines.items()}
            for name, output in outputs.items():
                try:
                    verify_canonical_output(case, output)
                except AssertionError as error:
                    failures.append(f"{name}/{case['name']}: {error}")
            rows.append({"case": case["name"], "gate": "independent-canonical", "engines": outputs})
        for case in principal_policy_cases(api):
            outputs = {name: engine.call({"action": "validate_policy", "data": api.canonical(case["policy"])})
                       for name, engine in engines.items()}
            for name, output in outputs.items():
                if case["valid"]:
                    if output != {"valid": True}:
                        failures.append(f"{name}/{case['name']}: expected valid numeric principal, got {output}")
                elif output.get("error", "").split(":", 1)[0] != case["error_code"]:
                    failures.append(f"{name}/{case['name']}: expected {case['error_code']}, got {output}")
            rows.append({"case": case["name"], "gate": "independent-principal-policy", "engines": outputs})
        for case in structural_wire_cases(source, api):
            text = api.canonical(case["record"])
            outputs = {name: engine.call({"action": "validate_commit", "data": text})
                       for name, engine in engines.items()}
            for name, output in outputs.items():
                if case["valid"]:
                    if output != {"canonical": text}:
                        failures.append(f"{name}/{case['name']}: expected exact structurally valid commit, got {output}")
                elif "error" not in output:
                    failures.append(f"{name}/{case['name']}: accepted structurally invalid commit")
            rows.append({"case": case["name"], "gate": "independent-structural-wire-not-authentication", "engines": outputs})
        for case in delivery_deadline_cases(source, deadlines, api):
            text = api.ledger_text(case["commits"])
            outputs = {name: engine.call(api.request(text, at=case["at"])) for name, engine in engines.items()}
            try:
                for name, output in outputs.items():
                    if case["expected"] != "allowed":
                        assert output.get("error", "").split(":", 1)[0] == case["expected"], (
                            f"{name}: expected {case['expected']}, got {output}")
                    else:
                        assert "error" not in output, f"{name}: {output}"
                        assert output["canonical_ledger"] == text, f"{name}: exact deadline ledger bytes"
                        work = output["projection"]["works"][case["work_id"]]
                        assert work["state"] == "completed" and work["barrier"] == "failed", (
                            f"{name}: expected completed/failed barrier, got {work['state']}/{work['barrier']}")
                if case["expected"] == "allowed":
                    api.assert_native_parity(outputs["go"], outputs["js"])
            except (AssertionError, KeyError) as error:
                failures.append(f"delivery-deadline-{case['name']}: {error}")
            rows.append({"case": case["name"], "gate": "independent-delivery-deadline-replay", "engines": outputs})
        for case in reasons["cases"]:
            ledger = reason_ledger(case["reason"], api)
            outputs = {name: engine.call(api.request(ledger.text())) for name, engine in engines.items()}
            for name, output in outputs.items():
                accepted = "error" not in output
                if accepted != case["valid"]:
                    failures.append(f"{name}/reason-{case['name']}: expected validity {case['valid']}, got {output}")
            rows.append({"case": f"reason-{case['name']}", "gate": "independent-reason-replay", "engines": outputs})
        for decimal, cases in ((False, identities["identity"]), (True, identities["decimal"])):
            for case in cases:
                value = "9" * case["digits"] if decimal else case["text"] * case["repeat"]
                if not decimal:
                    assert len(value.encode()) == case["expected_utf8_bytes"]
                ledger = identity_ledger(value, decimal, api)
                outputs = {name: engine.call(api.request(ledger.text())) for name, engine in engines.items()}
                for name, output in outputs.items():
                    accepted = "error" not in output
                    if accepted != case["valid"]:
                        failures.append(f"{name}/identity-{case['name']}: expected validity {case['valid']}, got {output}")
                    elif accepted and output["canonical_ledger"] != ledger.text():
                        failures.append(f"{name}/identity-{case['name']}: valid identity bytes changed")
                rows.append({"case": f"identity-{case['name']}", "gate": "independent-identity-replay", "engines": outputs})
        for end, expected in enumerate(source["prefixes"], 1):
            prefix = source["commits"][:end]
            ordered = api.ledger_text(prefix)
            # Cold restart, physical reversal and duplicate transport record.
            variants = {"ordered": ordered, "restart-reversed": api.ledger_text(list(reversed(prefix)) + [prefix[-1]])}
            for variant, text in variants.items():
                outputs = {name: engine.call(api.request(text, at=prefix[-1]["at"])) for name, engine in engines.items()}
                try:
                    for name, output in outputs.items():
                        verify_prefix(output, expected)
                        assert output["canonical_ledger"] == api.ordered_ledger(ordered), f"{name}: canonical prefix"
                        if end >= 4:
                            original = output["projection"]["dispatches"][source["assignment"]["dispatch_id"]]
                            fields = source["assignment"].keys()
                            assert {key: original[key] for key in fields} == source["assignment"]
                    api.assert_native_parity(outputs["go"], outputs["js"])
                except (AssertionError, KeyError) as error:
                    failures.append(f"prefix-{end}/{variant}: {error}")
                rows.append({"case": f"prefix-{end}/{variant}", "gate": "independent-restart-prefix", "engines": outputs})
        # Additional independent edge answers are literals/rational integers, not
        # generated from Go or JS selection. Shared selection answers stay intact.
        selections += [shared_selection_case(case, api) for case in extra["selection"]] + [byte_packing_case()] + [
            {"name": "UTF8-account-tie", "mode": "weighted-priority", "class_weights": [8, 4, 2, 1, 1],
             "accounting_weights": {"": 1, "𐀀": 1, "": 1},
             "nodes": [{"key": "astral", "priority": 3, "account": "𐀀", "profile": "default", "enqueued": 0},
                       {"key": "private-use", "priority": 3, "account": "", "profile": "default", "enqueued": 0}],
             "max_claims": 1, "max_dispatches": 1, "profile_max": 16, "share_keys": True,
             "expected": ["private-use"], "reason": "claim_budget_reached"},
            {"name": "integer-pass-beyond-IEEE754", "mode": "weighted-priority", "class_weights": [8, 4, 2, 1, 1],
             "accounting_weights": {"": 1, **{str(weight): weight for weight in [997, 991, 983, 977, 971, 967, 953]}},
             "nodes": [{"key": str(weight), "priority": 3, "account": str(weight), "profile": "default", "enqueued": 0}
                       for weight in [997, 991, 983, 977, 971, 967, 953]],
             "max_claims": 1, "max_dispatches": 1, "profile_max": 16, "share_keys": True,
             "expected": ["997"], "reason": "claim_budget_reached",
             "key_pass": str(math.lcm(997, 991, 983, 977, 971, 967, 953) // 997)},
        ]
        for case in selections:
            ledger, nodes = selection_ledger(case, api)
            params = {"pool": "default", "max_claims": case["max_claims"], "max_dispatches": case["max_dispatches"],
                      "max_bytes": case.get("max_bytes", 49152)}
            outputs = {name: engine.call(api.request(ledger.text(), parameters=params, request_id="independent",
                                                     commit_id="candidate", at=3)) for name, engine in engines.items()}
            expected_operations, expected_assignments = packing_oracle(case, nodes, api, "independent", "candidate")
            try:
                for name, output in outputs.items():
                    assert "error" not in output, f"{name}: {output}"
                    assert output["packing"]["operations"] == expected_operations, f"{name}: exact fair-prefix operations"
                    assert output["packing"]["assignments"] == expected_assignments, f"{name}: exact immutable packing"
                    assert output["packing"]["reason"] == case["reason"]
                    assert output["selection"]["work_id"] == nodes[case["expected"][0]]["work_id"]
                    if "key_pass" in case:
                        assert output["selection"]["key_pass"] == case["key_pass"]
                        assert int(case["key_pass"]) > 9007199254740991
                api.assert_native_parity(outputs["go"], outputs["js"])
            except (AssertionError, KeyError) as error:
                failures.append(f"{case['name']}: {error}")
            rows.append({"case": case["name"], "gate": "independent-selection-packing", "engines": outputs})
    after_hashes = {file: hashlib.sha256(paths[file].read_bytes()).hexdigest() for file in raw}
    if hashes != after_hashes:
        failures.append("Independent fixture files changed during verification; rerun against stable inputs.")
    report = {"status": "failed" if failures else "passed", "cases": rows, "failures": failures,
              "fixture_sha256": hashes, "fixture_sha256_after": after_hashes,
              "unmet_gates": ["Shared fixtures are bounded; not arbitrary full-wire/host/credential refinement."]}
    (evidence / "conformance.json").write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    return report
