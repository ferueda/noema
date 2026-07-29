import { readFile, readdir } from "node:fs/promises";

import { Ajv2020 } from "ajv/dist/2020.js";
import addFormatsModule from "ajv-formats";
import { describe, expect, it } from "vitest";

type JsonObject = Record<string, unknown>;

const CONTRACTS_ROOT = new URL("../../../contracts/", import.meta.url);
const addFormats = addFormatsModule.default;

async function readJson(relativePath: string): Promise<JsonObject> {
  const contents = await readFile(new URL(relativePath, CONTRACTS_ROOT), "utf8");
  return JSON.parse(contents) as JsonObject;
}

async function fixtureNames(relativeDirectory: string): Promise<string[]> {
  const entries = await readdir(new URL(relativeDirectory, CONTRACTS_ROOT));
  return entries.filter((entry) => entry.endsWith(".json")).sort();
}

async function expectFixtureSet(
  schemaPath: string,
  fixtureDirectory: string,
  fixturePrefix: string,
): Promise<void> {
  // The frozen schemas sometimes establish a property's type outside the
  // conditional subschema where its length is refined.
  const ajv = new Ajv2020({
    allErrors: true,
    strict: true,
    strictTypes: false,
  });
  addFormats(ajv);
  const validate = ajv.compile(await readJson(schemaPath));

  const matchingFixtures = (await fixtureNames(fixtureDirectory)).filter(
    (name) => name.startsWith(`${fixturePrefix}.`),
  );
  for (const fixtureName of matchingFixtures) {
    const fixtureUrl = new URL(
      `${fixtureDirectory}/${fixtureName}`,
      CONTRACTS_ROOT,
    );
    const contents = await readFile(fixtureUrl, "utf8");
    const shouldBeValid = fixtureName.includes(".valid");

    let parsed: JsonObject;
    try {
      parsed = JSON.parse(contents) as JsonObject;
    } catch (error) {
      expect(shouldBeValid, `${fixtureName} must contain valid JSON`).toBe(false);
      expect(error).toBeInstanceOf(SyntaxError);
      continue;
    }

    expect(
      validate(parsed),
      `${fixtureName}: ${ajv.errorsText(validate.errors)}`,
    ).toBe(shouldBeValid);
  }
}

describe("frozen agent execution contract", () => {
  it("accepts and rejects the request fixtures as frozen", async () => {
    await expectFixtureSet(
      "agent-execution/v1/request.schema.json",
      "agent-execution/v1/fixtures",
      "request",
    );
  });

  it("accepts and rejects the response fixtures as frozen", async () => {
    await expectFixtureSet(
      "agent-execution/v1/response.schema.json",
      "agent-execution/v1/fixtures",
      "response",
    );
  });
});

describe("frozen Content Scout contract", () => {
  it("accepts and rejects the input fixtures as frozen", async () => {
    await expectFixtureSet(
      "agents/content-scout/v1/input.schema.json",
      "agents/content-scout/v1/fixtures",
      "input",
    );
  });

  it("accepts and rejects the candidate fixtures as frozen", async () => {
    await expectFixtureSet(
      "agents/content-scout/v1/candidates.schema.json",
      "agents/content-scout/v1/fixtures",
      "candidates",
    );
  });
});
