import type { Page } from "@playwright/test";

export type JsonLdEntity = Record<string, unknown> & { "@type"?: string | string[] };

/** Collect entities across JSON-LD scripts, flattening arrays and @graph containers. */
export async function jsonLdEntities(page: Page): Promise<JsonLdEntity[]> {
  const scripts = await page.locator('script[type="application/ld+json"]').allTextContents();
  return scripts.flatMap((raw, index) => {
    if (!raw.trim()) throw new Error(`JSON-LD script #${index} is empty`);
    const data: unknown = JSON.parse(raw);
    const roots = Array.isArray(data) ? data : [data];
    return roots.flatMap(root => {
      const entity = asEntity(root);
      const graph = entity["@graph"];
      if (graph === undefined) return [entity];
      if (!Array.isArray(graph)) throw new Error(`JSON-LD script #${index} has a non-array @graph`);
      return graph.map(asEntity);
    });
  });
}

function asEntity(value: unknown): JsonLdEntity {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error(`Expected a JSON-LD object, got ${JSON.stringify(value)}`);
  }
  return value as JsonLdEntity;
}

export function entitiesOfType(entities: JsonLdEntity[], type: string): JsonLdEntity[] {
  return entities.filter(entity => [entity["@type"]].flat().includes(type));
}
