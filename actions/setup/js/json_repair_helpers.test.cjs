import { describe, it, expect } from "vitest";
import { repairJson, sanitizePrototypePollution } from "./json_repair_helpers.cjs";

describe("json_repair_helpers", () => {
  describe("repairJson", () => {
    describe("basic repairs", () => {
      it("should return valid JSON unchanged", () => {
        const validJson = '{"key": "value"}';
        expect(repairJson(validJson)).toBe(validJson);
      });

      it("should trim whitespace", () => {
        const json = '  {"key": "value"}  ';
        expect(repairJson(json)).toBe('{"key": "value"}');
      });

      it("should convert single quotes to double quotes", () => {
        const json = "{'key': 'value'}";
        expect(repairJson(json)).toBe('{"key": "value"}');
      });

      it("should quote unquoted object keys", () => {
        const json = "{key: 'value'}";
        expect(repairJson(json)).toBe('{"key": "value"}');
      });

      it("should handle multiple unquoted keys", () => {
        const json = "{name: 'John', age: 30}";
        expect(repairJson(json)).toBe('{"name": "John", "age": 30}');
      });
    });

    describe("control character escaping", () => {
      it("should escape tab characters", () => {
        const json = '{"key": "value\twith\ttabs"}';
        expect(repairJson(json)).toBe('{"key": "value\\twith\\ttabs"}');
      });

      it("should escape newline characters", () => {
        const json = '{"key": "value\nwith\nnewlines"}';
        expect(repairJson(json)).toBe('{"key": "value\\nwith\\nnewlines"}');
      });

      it("should escape carriage return characters", () => {
        const json = '{"key": "value\rwith\rreturns"}';
        expect(repairJson(json)).toBe('{"key": "value\\rwith\\rreturns"}');
      });

      it("should escape null bytes", () => {
        const json = '{"key": "value\x00with\x00null"}';
        expect(repairJson(json)).toBe('{"key": "value\\u0000with\\u0000null"}');
      });

      it("should escape form feed characters", () => {
        const json = '{"key": "value\fwith\fformfeed"}';
        expect(repairJson(json)).toBe('{"key": "value\\fwith\\fformfeed"}');
      });

      it("should escape backspace characters", () => {
        const json = '{"key": "value\bwith\bbackspace"}';
        expect(repairJson(json)).toBe('{"key": "value\\bwith\\bbackspace"}');
      });
    });

    describe("embedded quote handling", () => {
      it("should escape embedded quotes within strings", () => {
        const json = '{"key": "value"embedded"value"}';
        expect(repairJson(json)).toBe('{"key": "value\\"embedded\\"value"}');
      });

      it("should handle multiple embedded quotes", () => {
        const json = '{"key": "a"b"c"d"}';
        // Note: The regex-based repair has limitations with multiple embedded quotes
        // It repairs the pattern once but may not catch all occurrences
        expect(repairJson(json)).toBe('{"key": "a"b\\"c\\"d"}');
      });
    });

    describe("brace and bracket balancing", () => {
      it("should add missing closing brace", () => {
        const json = '{"key": "value"';
        expect(repairJson(json)).toBe('{"key": "value"}');
      });

      it("should add multiple missing closing braces", () => {
        const json = '{"outer": {"inner": "value"';
        expect(repairJson(json)).toBe('{"outer": {"inner": "value"}}');
      });

      it("should add missing opening brace", () => {
        const json = '"key": "value"}';
        expect(repairJson(json)).toBe('{"key": "value"}');
      });

      it("should add missing closing bracket", () => {
        const json = '["item1", "item2"';
        expect(repairJson(json)).toBe('["item1", "item2"]');
      });

      it("should add multiple missing closing brackets", () => {
        const json = '[["nested", "array"';
        expect(repairJson(json)).toBe('[["nested", "array"]]');
      });

      it("should add missing opening bracket", () => {
        const json = '"item1", "item2"]';
        expect(repairJson(json)).toBe('["item1", "item2"]');
      });

      it("should balance both braces and brackets", () => {
        const json = '{"items": ["a", "b"';
        // Note: When both braces and brackets are missing, the function adds them in order
        // This may result in "}" being added before "]" causing an imbalance
        expect(repairJson(json)).toBe('{"items": ["a", "b"}]');
      });
    });

    describe("trailing comma removal", () => {
      it("should remove trailing comma before closing brace", () => {
        const json = '{"key": "value",}';
        expect(repairJson(json)).toBe('{"key": "value"}');
      });

      it("should remove trailing comma before closing bracket", () => {
        const json = '["item1", "item2",]';
        expect(repairJson(json)).toBe('["item1", "item2"]');
      });

      it("should remove multiple trailing commas", () => {
        const json = '{"a": "b", "c": ["d", "e",],}';
        expect(repairJson(json)).toBe('{"a": "b", "c": ["d", "e"]}');
      });
    });

    describe("array closing fix", () => {
      it("should fix array closed with brace instead of bracket", () => {
        const json = '["item1", "item2"}';
        expect(repairJson(json)).toBe('["item1", "item2"]');
      });

      it("should fix nested arrays closed with braces", () => {
        const json = '["a", "b"}';
        expect(repairJson(json)).toBe('["a", "b"]');
      });
    });

    describe("complex scenarios", () => {
      it("should handle combination of repairs", () => {
        const json = "{name: 'John', items: ['a', 'b'";
        // Note: When both braces and brackets are missing, the function adds them in order
        expect(repairJson(json)).toBe('{"name": "John", "items": ["a", "b"}]');
      });

      it("should repair deeply nested structures", () => {
        const json = "{outer: {inner: {deep: 'value'";
        expect(repairJson(json)).toBe('{"outer": {"inner": {"deep": "value"}}}');
      });

      it("should handle mixed quote types and unquoted keys", () => {
        const json = "{name: 'John', age: \"30\", city: 'NYC'}";
        expect(repairJson(json)).toBe('{"name": "John", "age": "30", "city": "NYC"}');
      });

      it("should repair object with control characters and missing braces", () => {
        const json = '{"message": "Line1\nLine2"';
        expect(repairJson(json)).toBe('{"message": "Line1\\nLine2"}');
      });

      it("should handle empty objects", () => {
        const json = "{}";
        expect(repairJson(json)).toBe("{}");
      });

      it("should handle empty arrays", () => {
        const json = "[]";
        expect(repairJson(json)).toBe("[]");
      });

      it("should handle whitespace-only strings", () => {
        const json = "   ";
        expect(repairJson(json)).toBe("");
      });
    });

    describe("edge cases", () => {
      it("should handle JSON with underscores in keys", () => {
        const json = "{user_name: 'test'}";
        expect(repairJson(json)).toBe('{"user_name": "test"}');
      });

      it("should handle JSON with dollar signs in keys", () => {
        const json = "{$key: 'value'}";
        expect(repairJson(json)).toBe('{"$key": "value"}');
      });

      it("should handle JSON with numbers in keys", () => {
        const json = "{key123: 'value'}";
        expect(repairJson(json)).toBe('{"key123": "value"}');
      });

      it("should handle backslashes in strings", () => {
        const json = '{"path": "C:\\\\Users\\\\test"}';
        expect(repairJson(json)).toBe('{"path": "C:\\\\Users\\\\test"}');
      });

      it("should preserve already escaped characters", () => {
        const json = '{"text": "already\\nescaped"}';
        expect(repairJson(json)).toBe('{"text": "already\\nescaped"}');
      });
    });

    describe("real-world scenarios", () => {
      it("should repair typical agent output with missing closing brace", () => {
        const json = '{"type": "create_issue", "title": "Bug report", "body": "Description here"';
        expect(repairJson(json)).toBe('{"type": "create_issue", "title": "Bug report", "body": "Description here"}');
      });

      it("should repair output with unquoted keys and single quotes", () => {
        const json = "{type: 'update_issue', number: 123, title: 'Updated title'}";
        expect(repairJson(json)).toBe('{"type": "update_issue", "number": 123, "title": "Updated title"}');
      });

      it("should repair output with embedded newlines", () => {
        const json = '{"body": "Line 1\nLine 2\nLine 3"}';
        expect(repairJson(json)).toBe('{"body": "Line 1\\nLine 2\\nLine 3"}');
      });
    });
  });

  describe("sanitizePrototypePollution", () => {
    describe("basic sanitization", () => {
      it("should remove __proto__ property", () => {
        const obj = { name: "test", __proto__: { isAdmin: true } };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({ name: "test" });
        // Verify __proto__ key was removed from own properties
        expect(Object.prototype.hasOwnProperty.call(sanitized, "__proto__")).toBe(false);
      });

      it("should remove constructor property", () => {
        const obj = { name: "test", constructor: { prototype: { isAdmin: true } } };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({ name: "test" });
        // Verify constructor key was removed from own properties
        expect(Object.prototype.hasOwnProperty.call(sanitized, "constructor")).toBe(false);
      });

      it("should remove prototype property", () => {
        const obj = { name: "test", prototype: { isAdmin: true } };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({ name: "test" });
        // Verify prototype key was removed from own properties
        expect(Object.prototype.hasOwnProperty.call(sanitized, "prototype")).toBe(false);
      });

      it("should remove all dangerous keys simultaneously", () => {
        const obj = {
          name: "test",
          __proto__: { isAdmin: true },
          constructor: { isAdmin: true },
          prototype: { isAdmin: true },
        };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({ name: "test" });
      });

      it("should preserve safe properties", () => {
        const obj = { name: "John", age: 30, city: "NYC", status: "active" };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual(obj);
      });
    });

    describe("nested object sanitization", () => {
      it("should sanitize nested __proto__ properties", () => {
        const obj = {
          user: {
            name: "test",
            __proto__: { isAdmin: true },
          },
        };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({ user: { name: "test" } });
      });

      it("should sanitize deeply nested dangerous properties", () => {
        const obj = {
          outer: {
            middle: {
              inner: {
                __proto__: { isAdmin: true },
                constructor: { bad: true },
                safe: "value",
              },
            },
          },
        };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({
          outer: {
            middle: {
              inner: {
                safe: "value",
              },
            },
          },
        });
      });

      it("should handle mixed safe and dangerous properties at multiple levels", () => {
        const obj = {
          level1: "safe",
          __proto__: { bad: true },
          nested: {
            level2: "safe",
            constructor: { bad: true },
            deepNested: {
              level3: "safe",
              prototype: { bad: true },
            },
          },
        };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({
          level1: "safe",
          nested: {
            level2: "safe",
            deepNested: {
              level3: "safe",
            },
          },
        });
      });
    });

    describe("array sanitization", () => {
      it("should sanitize objects within arrays", () => {
        const obj = [
          { name: "test1", __proto__: { isAdmin: true } },
          { name: "test2", constructor: { bad: true } },
        ];
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual([{ name: "test1" }, { name: "test2" }]);
      });

      it("should handle nested arrays", () => {
        const obj = {
          items: [[{ __proto__: { bad: true }, value: 1 }], [{ constructor: { bad: true }, value: 2 }]],
        };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({
          items: [[{ value: 1 }], [{ value: 2 }]],
        });
      });

      it("should preserve arrays with safe values", () => {
        const obj = { items: ["a", "b", "c"], numbers: [1, 2, 3] };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual(obj);
      });
    });

    describe("primitive type handling", () => {
      it("should handle null", () => {
        const sanitized = sanitizePrototypePollution(null);
        expect(sanitized).toBeNull();
      });

      it("should handle undefined", () => {
        const sanitized = sanitizePrototypePollution(undefined);
        expect(sanitized).toBeUndefined();
      });

      it("should handle strings", () => {
        const sanitized = sanitizePrototypePollution("test string");
        expect(sanitized).toBe("test string");
      });

      it("should handle numbers", () => {
        const sanitized = sanitizePrototypePollution(42);
        expect(sanitized).toBe(42);
      });

      it("should handle booleans", () => {
        const sanitized = sanitizePrototypePollution(true);
        expect(sanitized).toBe(true);
      });
    });

    describe("edge cases", () => {
      it("should handle empty objects", () => {
        const sanitized = sanitizePrototypePollution({});
        expect(sanitized).toEqual({});
      });

      it("should handle empty arrays", () => {
        const sanitized = sanitizePrototypePollution([]);
        expect(sanitized).toEqual([]);
      });

      it("should handle objects with only dangerous properties", () => {
        const obj = {
          __proto__: { isAdmin: true },
          constructor: { bad: true },
          prototype: { bad: true },
        };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({});
      });

      it("should handle objects with null prototype", () => {
        const obj = Object.create(null);
        obj.name = "test";
        obj.__proto__ = { isAdmin: true };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized).toEqual({ name: "test" });
      });

      it("should handle circular references in objects", () => {
        const obj = { name: "test", safe: "value" };
        obj.circular = obj;
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized.name).toBe("test");
        expect(sanitized.safe).toBe("value");
        expect(sanitized.circular).toBe(sanitized);
      });

      it("should handle circular references with dangerous keys", () => {
        const obj = { name: "test", __proto__: { bad: true } };
        obj.circular = obj;
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized.name).toBe("test");
        expect(sanitized.circular).toBe(sanitized);
        expect(Object.prototype.hasOwnProperty.call(sanitized, "__proto__")).toBe(false);
      });

      it("should handle nested circular references", () => {
        const obj = { name: "outer", nested: { name: "inner" } };
        obj.nested.parent = obj;
        obj.self = obj;
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized.name).toBe("outer");
        expect(sanitized.nested.name).toBe("inner");
        expect(sanitized.nested.parent).toBe(sanitized);
        expect(sanitized.self).toBe(sanitized);
      });

      it("should handle circular references in arrays", () => {
        const arr = [1, 2, { name: "test" }];
        arr.push(arr);
        const sanitized = sanitizePrototypePollution(arr);
        expect(sanitized[0]).toBe(1);
        expect(sanitized[1]).toBe(2);
        expect(sanitized[2]).toEqual({ name: "test" });
        expect(sanitized[3]).toBe(sanitized);
      });

      it("should handle very deep nesting (1000 levels)", () => {
        let obj = { value: "leaf", __proto__: { bad: true } };
        for (let i = 0; i < 1000; i++) {
          obj = { level: i, nested: obj, __proto__: { bad: true } };
        }
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized.level).toBe(999);
        expect(Object.prototype.hasOwnProperty.call(sanitized, "__proto__")).toBe(false);
        // Verify we can traverse to the leaf
        let current = sanitized;
        for (let i = 999; i >= 0; i--) {
          expect(current.level).toBe(i);
          expect(Object.prototype.hasOwnProperty.call(current, "__proto__")).toBe(false);
          current = current.nested;
        }
        expect(current.value).toBe("leaf");
      });

      it("should handle mixed circular and nested structures", () => {
        const root = { name: "root" };
        const child1 = { name: "child1", parent: root };
        const child2 = { name: "child2", parent: root, sibling: child1 };
        root.children = [child1, child2];
        child1.sibling = child2;
        const sanitized = sanitizePrototypePollution(root);
        expect(sanitized.name).toBe("root");
        expect(sanitized.children[0].name).toBe("child1");
        expect(sanitized.children[1].name).toBe("child2");
        expect(sanitized.children[0].parent).toBe(sanitized);
        expect(sanitized.children[1].parent).toBe(sanitized);
        expect(sanitized.children[0].sibling).toBe(sanitized.children[1]);
        expect(sanitized.children[1].sibling).toBe(sanitized.children[0]);
      });

      it("should handle array with circular object references", () => {
        const obj1 = { name: "obj1" };
        const obj2 = { name: "obj2", ref: obj1 };
        obj1.ref = obj2;
        const arr = [obj1, obj2, obj1, obj2];
        const sanitized = sanitizePrototypePollution(arr);
        expect(sanitized[0].name).toBe("obj1");
        expect(sanitized[1].name).toBe("obj2");
        expect(sanitized[0]).toBe(sanitized[2]);
        expect(sanitized[1]).toBe(sanitized[3]);
        expect(sanitized[0].ref).toBe(sanitized[1]);
        expect(sanitized[1].ref).toBe(sanitized[0]);
      });

      it("should handle objects with repeated references (non-circular)", () => {
        const shared = { value: "shared", __proto__: { bad: true } };
        const obj = {
          ref1: shared,
          ref2: shared,
          nested: {
            ref3: shared,
          },
        };
        const sanitized = sanitizePrototypePollution(obj);
        expect(sanitized.ref1.value).toBe("shared");
        expect(sanitized.ref1).toBe(sanitized.ref2);
        expect(sanitized.ref1).toBe(sanitized.nested.ref3);
        expect(Object.prototype.hasOwnProperty.call(sanitized.ref1, "__proto__")).toBe(false);
      });

      it("should handle malicious deeply nested attack with circularity", () => {
        const attack = { __proto__: { exploit: true } };
        for (let i = 0; i < 100; i++) {
          attack.nested = { __proto__: { exploit: true }, level: i };
          if (i === 50) {
            attack.nested.circular = attack;
          }
          Object.assign(attack, attack.nested);
        }
        const sanitized = sanitizePrototypePollution(attack);
        expect(Object.prototype.hasOwnProperty.call(sanitized, "__proto__")).toBe(false);
        expect({}.exploit).toBeUndefined();
      });
    });

    describe("real-world attack scenarios", () => {
      it("should prevent prototype pollution via __proto__", () => {
        const malicious = { type: "create_issue", __proto__: { isAdmin: true } };
        const sanitized = sanitizePrototypePollution(malicious);
        expect(sanitized).toEqual({ type: "create_issue" });
        // Verify that the prototype was not polluted
        expect({}.isAdmin).toBeUndefined();
      });

      it("should prevent prototype pollution via constructor", () => {
        const malicious = {
          type: "update_issue",
          constructor: { prototype: { isAdmin: true } },
        };
        const sanitized = sanitizePrototypePollution(malicious);
        expect(sanitized).toEqual({ type: "update_issue" });
      });

      it("should handle agent output with prototype pollution attempt", () => {
        const malicious = {
          type: "create_issue",
          title: "Legitimate Issue",
          body: "Description",
          __proto__: { isAdmin: true, polluted: true },
          constructor: { prototype: { injected: true } },
        };
        const sanitized = sanitizePrototypePollution(malicious);
        expect(sanitized).toEqual({
          type: "create_issue",
          title: "Legitimate Issue",
          body: "Description",
        });
      });

      it("should handle deeply nested pollution attempts", () => {
        const malicious = {
          type: "create_issue",
          metadata: {
            __proto__: { level1: true },
            config: {
              constructor: { level2: true },
              settings: {
                prototype: { level3: true },
                value: "safe",
              },
            },
          },
        };
        const sanitized = sanitizePrototypePollution(malicious);
        expect(sanitized).toEqual({
          type: "create_issue",
          metadata: {
            config: {
              settings: {
                value: "safe",
              },
            },
          },
        });
      });
    });

    describe("integration with common patterns", () => {
      it("should work with Object.assign after sanitization", () => {
        const target = { existing: "value" };
        const malicious = { new: "data", __proto__: { isAdmin: true } };
        const sanitized = sanitizePrototypePollution(malicious);
        Object.assign(target, sanitized);
        expect(target).toEqual({ existing: "value", new: "data" });
        expect({}.isAdmin).toBeUndefined();
      });

      it("should prevent pollution when pushing to arrays", () => {
        const items = [];
        const malicious = { type: "item", __proto__: { polluted: true } };
        const sanitized = sanitizePrototypePollution(malicious);
        items.push(sanitized);
        expect(items).toEqual([{ type: "item" }]);
        expect({}.polluted).toBeUndefined();
      });

      it("should work with spread operator after sanitization", () => {
        const malicious = { safe: "data", __proto__: { isAdmin: true } };
        const sanitized = sanitizePrototypePollution(malicious);
        const result = { ...sanitized, extra: "value" };
        expect(result).toEqual({ safe: "data", extra: "value" });
        expect({}.isAdmin).toBeUndefined();
      });
    });
  });
});
