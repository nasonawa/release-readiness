import { expect, test } from "@playwright/test";

const names = ["quay-v3.18.4", "quay-v3.18.3", "quay-v3.18.2", "omr-v2.0.13"];
const validation = {
	plan_known: true,
	revision: "reviewed-plan",
	source_fresh: true,
	ready: false,
	total: 2,
	passed: 1,
	failed: 0,
	running: 0,
	missing: 1,
	rows: [
		{
			job: "job-a",
			display_name: "Install · FIPS",
			ocp: "4.18",
			cloud: "AWS",
			storage: "S3",
			architecture: "amd64",
			mode: "Operator",
			source_url: "https://github.com/openshift/release",
			state: "passed",
			run_url: "https://prow.example/run/1",
		},
		{
			job: "job-b",
			display_name: "Install · ARM64",
			ocp: "4.19",
			cloud: "GCP",
			storage: "GCS",
			architecture: "arm64",
			mode: "Operator",
			source_url: "https://github.com/openshift/release",
			state: "missing",
			evidence: "The latest observed run tested a different FBC; no verified match was found.",
			run_url: "https://prow.example/run/other",
		},
	],
};
const releases = names.map((name, index) => ({
	release: {
		name,
		released: index === 1 || index === 2,
		release_ticket_key: `PROJQUAY-${100 + index}`,
		konflux_application: name.startsWith("quay") ? "quay-3-18" : "omr-2-0",
	},
	published: index === 1 || index === 2,
	shipped: index === 1 || index === 2,
	next_in_stream: index === 0 || index === 3,
	shipment_mr: null,
	readiness: { signal: "", message: "" },
	portfolio: {
		stage: index === 1 || index === 2 ? "available" : "validation",
		verdict: { state: "not_ready", items: [] },
		validation,
	},
}));

test.beforeEach(async ({ page }) => {
	await page.route("**/api/v1/**", async (route) => {
		const path = new URL(route.request().url()).pathname;
		if (path.endsWith("/config"))
			return route.fulfill({
				json: {
					app_version: "2.0.0-dev",
					jira_base_url: "https://jira.example",
					jira_project: "PROJQUAY",
					jira_enabled: true,
					konflux_ui_url: "https://konflux.example",
					konflux_namespace: "art-quay-tenant",
				},
			});
		if (path.endsWith("/sync-status"))
			return route.fulfill({ json: { sources: [], problems: [] } });
		if (path.endsWith("/overview")) return route.fulfill({ json: releases });
		if (path.endsWith("/validation"))
			return route.fulfill({ json: validation });
		if (path.endsWith("/issues"))
			return route.fulfill({
				json: [
					{
						key: "PROJQUAY-999",
						summary: "Improve registry availability",
						status: "Release Pending",
						issue_type: "Bug",
						link: "https://jira.example/browse/PROJQUAY-999",
					},
				],
			});
		if (path.endsWith("/ci")) return route.fulfill({ json: [] });
		if (path.endsWith("/candidate"))
			return route.fulfill({
				json: {
					shipped: path.includes("3.18.3"),
					candidate: null,
					reason: "No staged candidate",
					fbc_candidate: null,
					fbc_stream: null,
					verdict: { state: "not_ready", items: [] },
					reading: { read: 0, failed: 0, pending: 0, history_days: 0 },
				},
			});
		const release = releases.find((r) => path.endsWith(r.release.name));
		if (release)
			return route.fulfill({
				json: {
					...release.release,
					published: release.published,
					shipment_mr: null,
				},
			});
		return route.fulfill({ status: 404, json: { error: "not found" } });
	});
});

test("stream cards expand to z-stream stages and preserve navigation", async ({
	page,
}, testInfo) => {
	const errors: string[] = [];
	page.on("pageerror", (e) => errors.push(e.message));
	await page.goto("/");
	await expect(
		page.getByRole("heading", { name: "Release portfolio", exact: true }),
	).toBeVisible();
	await expect(page.locator(".rr-card-grid article")).toHaveCount(2);
	await expect(
		page.getByRole("heading", { name: "Quay 3.18", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "OMR 2.0", exact: true }),
	).toBeVisible();
	await expect(page.getByText("Mock evidence")).toHaveCount(0);
	await page.getByRole("button", { name: "Stages", exact: true }).click();
	await expect(page.locator(".rr-stage-grid article")).toHaveCount(3);
	await page.getByText("Show history", { exact: true }).click();
	await expect(page.locator(".rr-stage-grid article")).toHaveCount(4);
	await page
		.getByRole("heading", { name: "Quay v3.18.3", exact: true })
		.getByRole("link")
		.click();
	await expect(
		page.getByRole("heading", { name: "Available to customers", exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("heading", { name: "FBC record for this release" }),
	).toBeVisible();
	await page
		.getByRole("tab", { name: "Validation workbench", exact: true })
		.click();
	await expect(
		page.getByRole("heading", { name: "Required validation matrix" }),
	).toBeVisible();
	await page
		.getByRole("combobox", { name: "Architecture", exact: true })
		.selectOption("arm64");
	await expect(page.getByText("GCP / GCS")).toBeVisible();
	await expect(page.getByRole("link", {name: "Install · ARM64 ↗", exact: true})).toBeVisible();
	await expect(page.getByText("The latest observed run tested a different FBC; no verified match was found.")).not.toBeVisible();
	await page.locator(".rr-validation-table summary").click();
	await expect(page.getByText("The latest observed run tested a different FBC; no verified match was found.")).toBeVisible();
	await expect(page.getByRole("link",{name:"Open observed run ↗"})).toHaveAttribute("href","https://prow.example/run/other");
	await expect(page.getByText("AWS / S3")).toHaveCount(0);
	await page.getByRole("tab", { name: "JIRA issues", exact: true }).click();
	await expect(
		page.getByRole("link", { name: "PROJQUAY-999", exact: true }),
	).toBeVisible();
	await page.reload();
	await expect(
		page.getByRole("tab", { name: "JIRA issues", exact: true }),
	).toHaveAttribute("aria-selected", "true");
	await page.getByRole("link", { name: "All releases", exact: true }).click();
	await expect(
		page.getByRole("button", { name: "Stages", exact: true }),
	).toHaveAttribute("aria-pressed", "true");
	await expect(
		page.getByRole("switch", { name: "Show history", exact: true }),
	).toBeChecked();
	await page.screenshot({
		path: testInfo.outputPath("stages.png"),
		fullPage: true,
	});
	expect(errors).toEqual([]);
});

test("accessible controls, mobile layout and reduced motion", async ({
	page,
}, testInfo) => {
	await page.emulateMedia({ reducedMotion: "reduce" });
	await page.goto("/");
	await page.getByRole("button", { name: "Stages", exact: true }).click();
	expect(await page.evaluate(() => document.getAnimations().length)).toBe(0);
	await page.getByRole("button", { name: "Cards", exact: true }).click();
	await page.getByRole("button", { name: "About this dashboard" }).click();
	await expect(
		page.getByText("Release readiness 2.0.0-dev", { exact: true }),
	).toBeVisible();
	await expect(
		page.getByRole("link", { name: "Send feedback or report an issue" }),
	).toHaveAttribute(
		"href",
		"https://github.com/quay/release-readiness/issues/new",
	);
	await page.keyboard.press("Escape");
	await page.getByRole("button", { name: "Switch to dark theme" }).click();
	await page.reload();
	await expect(
		page.getByRole("button", { name: "Switch to light theme" }),
	).toBeVisible();
	for (const width of [1440, 1024, 736, 390, 320]) {
		await page.setViewportSize({ width, height: 1000 });
		expect(
			await page.evaluate(
				() => document.documentElement.scrollWidth <= innerWidth,
			),
			`No horizontal overflow at ${width}px`,
		).toBe(true);
	}
	await page.screenshot({
		path: testInfo.outputPath("cards-mobile-dark.png"),
		fullPage: true,
	});
});

test("no configured jobs differs from unavailable inventory", async ({
	page,
}) => {
	const empty = { ...validation, total: 0, passed: 0, missing: 0, rows: [] };
	await page.route("**/api/v1/releases/overview?*", (route) =>
		route.fulfill({
			json: releases.map((r) => ({
				...r,
				portfolio: { ...r.portfolio, validation: empty },
			})),
		}),
	);
	await page.route("**/api/v1/releases/*/validation", (route) =>
		route.fulfill({ json: empty }),
	);
	await page.goto("/");
	await expect(
		page.locator(".rr-card-grid").getByText("No CI configured"),
	).toHaveCount(2);
	await page
		.getByRole("heading", { name: "Quay 3.18", exact: true })
		.getByRole("link")
		.click();
	await page
		.getByRole("tab", { name: "Validation workbench", exact: true })
		.click();
	await expect(
		page.getByRole("tabpanel").getByText("No CI configured", { exact: true }),
	).toBeVisible();
	await expect(
		page.getByText("CI readiness cannot be established.", { exact: false }),
	).toBeVisible();
	await page.route("**/api/v1/releases/*/validation", (route) =>
		route.fulfill({ json: { ...empty, plan_known: false } }),
	);
	await page.reload();
	await expect(
		page
			.getByRole("tabpanel")
			.getByRole("heading", { name: /CI configuration unavailable/ }),
	).toBeVisible();
});

test("long source-status popover stays within the viewport", async ({
	page,
}, testInfo) => {
	await page.route("**/api/v1/sync-status", (route) =>
		route.fulfill({
			json: {
				sources: [],
				problems: ["jira", "konflux", "prow", "fbc-render", "image-scans"].map(
					(source) => ({
						source,
						message: "Source failure: ".repeat(25),
						since: new Date().toISOString(),
						last_success: null,
					}),
				),
			},
		}),
	);
	for (const [width, height] of [
		[1366, 768],
		[720, 640],
		[390, 560],
	]) {
		await page.setViewportSize({ width, height });
		await page.goto("/");
		await page.getByRole("button", { name: /4 sources stale/ }).click();
		const popup = page.locator(".rr-source-popover:visible");
		await expect(popup).toBeVisible();
		await page.screenshot({
			path: testInfo.outputPath(`source-status-${width}.png`),
		});
		await expect(async () => {
			const box = await popup.boundingBox();
			expect(box).not.toBeNull();
			expect(box!.y).toBeGreaterThanOrEqual(0);
			expect(box!.x).toBeGreaterThanOrEqual(0);
			expect(box!.y + box!.height).toBeLessThanOrEqual(height);
			expect(box!.x + box!.width).toBeLessThanOrEqual(width);
		}).toPass({ timeout: 3000 });
		const content = popup.locator(".pf-v6-c-popover__content");
		expect(
			await content.evaluate((el) => el.scrollHeight > el.clientHeight),
		).toBe(true);
		await page.screenshot({
			path: testInfo.outputPath(`source-status-${width}.png`),
		});
		await page.keyboard.press("Escape");
		await expect(popup).not.toBeVisible();
	}
});

test("confirmed active CI is Running with compact evidence details", async ({ page }) => {
	await page.route("**/api/v1/releases/*/validation", (route) => route.fulfill({ json: {
		...validation, passed: 0, running: 1, missing: 0, total: 1,
		rows: [{ ...validation.rows[0], state: "running", evidence: "The selected-FBC run is still in progress." }],
	} }));
	await page.goto("/");
	await page.getByRole("heading", { name: "Quay 3.18", exact: true }).getByRole("link").click();
	await page.getByRole("tab", { name: "Validation workbench", exact: true }).click();
	const table = page.locator(".rr-validation-table");
	await expect(table.getByText("Running", { exact: true })).toBeVisible();
	await expect(table.getByText("Awaiting evidence", { exact: true })).toHaveCount(0);
	await expect(table.getByText("The selected-FBC run is still in progress.")).not.toBeVisible();
	await table.locator("summary").click();
	await expect(table.getByText("The selected-FBC run is still in progress.")).toBeVisible();
});

test("observed CI success stays separate from selected-FBC readiness", async ({ page }) => {
	await page.route("**/api/v1/releases/*/validation", (route) => route.fulfill({ json: {
		...validation, passed: 0, running: 0, missing: 2, total: 2,
		rows: [
			{ ...validation.rows[0], display_name: "Install · ARM64", state: "missing", ci_result: "passed", fbc_match: "newer",
				evidence: "This run tested a newer ART FBC than the selected candidate.",
				tested_fbc: {nvr:"newer-build", digest:"sha256:newer"}, selected_fbc: {nvr:"selected-build", digest:"sha256:selected"} },
			{ ...validation.rows[1], display_name: "Install · RDS", state: "missing", run_url: undefined, evidence: "No run has been ingested for this configured job." },
		],
	} }));
	await page.goto("/releases/quay-v3.18.4?view=workbench");
	const table = page.locator(".rr-validation-table");
	const arm = table.locator("tbody tr").filter({hasText:"Install · ARM64"});
	await expect(arm.getByText("Passed · newer FBC", {exact:true})).toBeVisible();
	await expect(arm.getByText("No matching run for selected FBC", {exact:true})).toBeVisible();
	await expect(table.locator("tbody tr").filter({hasText:"Install · RDS"}).getByText("No matching run for selected FBC", {exact:true})).toBeVisible();
	await expect(page.getByText("0 of 2", {exact:true})).toBeVisible();
	await arm.locator("summary").click();
	await expect(arm.getByText("sha256:newer", {exact:true})).toBeVisible();
	await expect(arm.getByText("sha256:selected", {exact:true})).toBeVisible();
	await page.getByRole("combobox", {name:"CI result",exact:true}).selectOption("passed");
	await expect(table.locator("tbody tr")).toHaveCount(1);
	await page.getByRole("combobox", {name:"CI result",exact:true}).selectOption("unmatched");
	await expect(table.locator("tbody tr")).toHaveCount(2);
});

test("source freshness uses only the shared hook polling interval", async ({ page }) => {
	await page.clock.install();
	let requests = 0;
	await page.route("**/api/v1/sync-status", (route) => {
		requests++;
		return route.fulfill({ json: { sources: [], problems: [] } });
	});
	await page.goto("/");
	await expect(page.getByRole("heading", { name: "Release portfolio" })).toBeVisible();
	await expect.poll(() => requests).toBeGreaterThan(0);
	requests = 0;
	await page.clock.runFor(30_000);
	expect(requests).toBe(0);
	await page.clock.runFor(30_000);
	await expect.poll(() => requests).toBeGreaterThan(0);
	// Desktop and compact masthead consumers each poll once per minute.
	expect(requests).toBeLessThanOrEqual(2);
});
