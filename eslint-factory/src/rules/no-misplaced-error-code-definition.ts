import { AST_NODE_TYPES, ESLintUtils, TSESTree } from "@typescript-eslint/utils";
import path from "node:path";

const createRule = ESLintUtils.RuleCreator(name => `https://github.com/github/gh-aw/tree/main/eslint-factory#${name}`);

const ERROR_CODE_NAME_PATTERN = /_(?:ERROR|REASON)_CODE$/;
const ERROR_CODES_FILENAME = "error_codes.cjs";

function isModuleExports(node: TSESTree.Node): boolean {
  return (
    node.type === AST_NODE_TYPES.MemberExpression && !node.computed && node.object.type === AST_NODE_TYPES.Identifier && node.object.name === "module" && node.property.type === AST_NODE_TYPES.Identifier && node.property.name === "exports"
  );
}

function getExportedPropertyName(node: TSESTree.MemberExpression): string | null {
  if (node.object.type === AST_NODE_TYPES.Identifier && node.object.name === "exports") {
    if (!node.computed && node.property.type === AST_NODE_TYPES.Identifier) return node.property.name;
    if (node.computed && node.property.type === AST_NODE_TYPES.Literal && typeof node.property.value === "string") return node.property.value;
  }

  if (node.object.type === AST_NODE_TYPES.MemberExpression && isModuleExports(node.object)) {
    if (!node.computed && node.property.type === AST_NODE_TYPES.Identifier) return node.property.name;
    if (node.computed && node.property.type === AST_NODE_TYPES.Literal && typeof node.property.value === "string") return node.property.value;
  }

  return null;
}

function getObjectPropertyName(node: TSESTree.Property): string | null {
  if (!node.computed && node.key.type === AST_NODE_TYPES.Identifier) return node.key.name;
  if (node.key.type === AST_NODE_TYPES.Literal && typeof node.key.value === "string") return node.key.value;
  return null;
}

export const noMisplacedErrorCodeDefinitionRule = createRule({
  name: "no-misplaced-error-code-definition",
  meta: {
    type: "suggestion",
    docs: {
      description: "Require exported error-code and reason-code constants to be defined in the centralized error_codes.cjs registry.",
    },
    schema: [],
    messages: {
      misplacedErrorCode: "Exported code '{{name}}' must be defined in error_codes.cjs and imported from there.",
    },
  },
  defaultOptions: [],
  create(context) {
    if (path.basename(context.filename) === ERROR_CODES_FILENAME) {
      return {};
    }

    const declarations = new Map<string, TSESTree.VariableDeclarator>();
    const exportedNames = new Map<string, string>();
    const directExportDefinitions: Array<{ name: string; node: TSESTree.Node }> = [];

    return {
      VariableDeclaration(node) {
        if (node.kind !== "const" || node.parent.type !== AST_NODE_TYPES.Program) return;

        for (const declaration of node.declarations) {
          if (declaration.id.type === AST_NODE_TYPES.Identifier) {
            declarations.set(declaration.id.name, declaration);
          }
        }
      },
      AssignmentExpression(node) {
        if (node.operator !== "=" || node.left.type !== AST_NODE_TYPES.MemberExpression) return;

        if (isModuleExports(node.left) && node.right.type === AST_NODE_TYPES.ObjectExpression) {
          for (const property of node.right.properties) {
            if (property.type !== AST_NODE_TYPES.Property) continue;
            const propertyName = getObjectPropertyName(property);
            const valueName = property.value.type === AST_NODE_TYPES.Identifier ? property.value.name : null;
            const codeName = propertyName && ERROR_CODE_NAME_PATTERN.test(propertyName) ? propertyName : valueName && ERROR_CODE_NAME_PATTERN.test(valueName) ? valueName : null;
            if (!codeName) continue;

            if (valueName) {
              exportedNames.set(valueName, codeName);
            } else {
              directExportDefinitions.push({ name: codeName, node: property });
            }
          }
          return;
        }

        const exportedPropertyName = getExportedPropertyName(node.left);
        if (!exportedPropertyName || !ERROR_CODE_NAME_PATTERN.test(exportedPropertyName)) return;

        if (node.right.type === AST_NODE_TYPES.Identifier) {
          exportedNames.set(node.right.name, exportedPropertyName);
        } else {
          directExportDefinitions.push({ name: exportedPropertyName, node });
        }
      },
      "Program:exit"() {
        for (const [localName, exportedName] of exportedNames) {
          const declaration = declarations.get(localName);
          if (!declaration) continue;
          context.report({ node: declaration, messageId: "misplacedErrorCode", data: { name: exportedName } });
        }

        for (const definition of directExportDefinitions) {
          context.report({
            node: definition.node,
            messageId: "misplacedErrorCode",
            data: { name: definition.name },
          });
        }
      },
    };
  },
});
