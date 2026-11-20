import assert from "node:assert/strict";
import { test } from "node:test";
import { apiBase } from "./client.ts";

test("apiBase joins the API path onto any base form", () => {
	assert.equal(apiBase("/"), "/api/v1");
	assert.equal(apiBase("/v2"), "/v2/api/v1");
	assert.equal(apiBase("/v2/"), "/v2/api/v1");
});
