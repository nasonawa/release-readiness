import type {
	DashboardConfig,
	FBCBuild,
	FBCTags,
	JiraIssue,
	ReleaseCandidate,
	ReleaseDetail,
	ReleaseOverview,
	SyncStatus,
	Validation,
} from "./types";

const BASE = "/api/v1";

async function fetchJSON<T>(url: string): Promise<T> {
	const res = await fetch(url);
	if (!res.ok) {
		throw new Error(`${res.status} ${res.statusText}`);
	}
	return res.json() as Promise<T>;
}

export function getConfig(): Promise<DashboardConfig> {
	return fetchJSON(`${BASE}/config`);
}

export function getSyncStatus(): Promise<SyncStatus> {
	return fetchJSON(`${BASE}/sync-status`);
}

// --- Release-centric API ---

export function listReleasesOverview(): Promise<ReleaseOverview[]> {
	return fetchJSON(`${BASE}/releases/overview?portfolio=1`);
}

export function listReleaseVersions(): Promise<ReleaseOverview[]> {
	return fetchJSON(`${BASE}/releases/overview`);
}

export function getRelease(version: string): Promise<ReleaseDetail> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}`);
}

export function listReleaseIssues(version: string): Promise<JiraIssue[]> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/issues`);
}

export function getCandidate(version: string): Promise<ReleaseCandidate> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/candidate`);
}

export function getFBCTags(version: string): Promise<FBCTags> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/fbc-tags`);
}

export function getValidation(version: string): Promise<Validation> {
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/validation`,
	);
}

export function getReleaseCI(version: string): Promise<FBCBuild[]> {
	return fetchJSON(`${BASE}/releases/${encodeURIComponent(version)}/ci`);
}

export function getCIHistory(
	version: string,
	job: string,
	includeOther: boolean,
	cursor: string,
): Promise<import("./types").CIHistoryPage> {
	const params = new URLSearchParams({
		scope: includeOther ? "all" : "selected",
		limit: "50",
	});
	if (job) params.set("job", job);
	if (cursor) params.set("cursor", cursor);
	return fetchJSON(
		`${BASE}/releases/${encodeURIComponent(version)}/ci-history?${params}`,
	);
}
