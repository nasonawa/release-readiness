import {
	Alert,
	Button,
	Label,
	Tab,
	Tabs,
	TabTitleText,
	Title,
} from "@patternfly/react-core";
import {
	CodeBranchIcon,
	JiraIcon,
	OpenshiftIcon,
	TasksIcon,
} from "@patternfly/react-icons";
import type { ReactNode } from "react";
import { useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { getValidation, listReleaseVersions } from "../api/client";
import type {
	DashboardConfig,
	ReleaseCandidate,
	ReleaseDetail,
	Validation,
	ValidationRow,
} from "../api/types";
import { useCachedFetch } from "../hooks/useCachedFetch";
import {
	ART_PIPELINES_HEALTH_URL,
	formatReleaseName,
	jiraSearchUrl,
	konfluxUrl,
	ticketsJql,
} from "../utils/links";
import { streamIdentity } from "../utils/portfolio";
import LoadError, { Loading } from "./LoadError";
import ValidationProgress from "./ValidationProgress";

export default function ReleaseWorkspace({
	release,
	config,
	candidate,
	candidateError,
	facts,
	builds,
	tickets,
	freshness,
}: {
	release: ReleaseDetail;
	config?: DashboardConfig;
	candidate?: ReleaseCandidate;
	candidateError?: Error;
	facts: ReactNode;
	builds: ReactNode;
	tickets: ReactNode;
	freshness: ReactNode;
}) {
	const [params, setParams] = useSearchParams();
	const navigate = useNavigate();
	const validation = useCachedFetch(`validation:${release.name}`, () =>
		getValidation(release.name),
	);
	const versions = useCachedFetch("releaseVersions", listReleaseVersions);
	const requested = params.get("view");
	const active =
		params.has("ci") || params.has("cell")
			? "workbench"
			: requested === "workbench" || requested === "issues"
				? requested
				: "briefing";
	const select = (view: string) =>
		setParams((prev) => {
			const next = new URLSearchParams(prev);
			next.set("view", view);
			next.delete("ci");
			next.delete("cell");
			return next;
		});
	const stream = streamIdentity(release.name);
	const konflux = konfluxUrl(
		config?.konflux_ui_url || "",
		config?.konflux_namespace || "",
		release.konflux_application || "",
	);
	const jira = config?.jira_base_url
		? jiraSearchUrl(
				ticketsJql(config.jira_project, release.name),
				config.jira_base_url,
			)
		: null;
	const blockers =
		candidate?.verdict.items.filter((i) => i.level === "blocker") || [];
	const published = release.published === true;
	const ready =
		!candidateError && !validation.error && validation.data?.ready === true;
	const label = published
		? "Available to customers"
		: ready
			? "Ready for shipment"
			: candidateError || validation.error
				? "Evidence unavailable"
				: !candidate
					? "Loading release evidence"
					: candidate.shipped
						? "Release recorded; publication unconfirmed"
						: blockers.length || validation.data?.failed
							? "Release needs attention"
							: "Validation in progress";
	return (
		<div className="rr-workspace">
			<div className="rr-version-nav">
				<div>
					<Title headingLevel="h1" size="3xl">
						{formatReleaseName(release.name)}
					</Title>
					{freshness}
				</div>
				<label>
					Switch release
					<select
						aria-label="Switch release"
						value={release.name}
						onChange={(e) => {
							const next = new URLSearchParams(params);
							next.delete("ci");
							next.delete("cell");
							navigate(
								`/releases/${encodeURIComponent(e.target.value)}?${next}`,
							);
						}}
					>
						{!versions.data?.some((v) => v.release.name === release.name) && (
							<option value={release.name}>
								{formatReleaseName(release.name)}
							</option>
						)}
						{versions.data
							?.slice()
							.sort((a, b) =>
								b.release.name.localeCompare(a.release.name, undefined, {
									numeric: true,
								}),
							)
							.map((v) => (
								<option value={v.release.name} key={v.release.name}>
									{formatReleaseName(v.release.name)}
								</option>
							))}
					</select>
				</label>
			</div>
			<nav aria-label="Release sources" className="rr-source-links">
				{stream.product === "quay" && (
					<a
						href={`https://sippy.dptools.openshift.org/sippy-ng/release/quay-${stream.version}`}
						target="_blank"
						rel="noreferrer"
					>
						<TasksIcon aria-hidden />
						Sippy ↗
					</a>
				)}
				{konflux && (
					<a href={konflux} target="_blank" rel="noreferrer">
						<OpenshiftIcon aria-hidden />
						Konflux ↗
					</a>
				)}
				<a href={ART_PIPELINES_HEALTH_URL} target="_blank" rel="noreferrer">
					<CodeBranchIcon aria-hidden />
					ART pipelines ↗
				</a>
				{jira && (
					<a href={jira} target="_blank" rel="noreferrer">
						<JiraIcon aria-hidden />
						JIRA ↗
					</a>
				)}
			</nav>
			<Tabs
				activeKey={active}
				onSelect={(_, key) => select(String(key))}
				aria-label="Release sections"
				mountOnEnter
				unmountOnExit
			>
				<Tab
					eventKey="briefing"
					title={<TabTitleText>Release briefing</TabTitleText>}
				>
					<section
						className={`rr-decision ${published ? "published" : ready ? "ready" : ""}`}
						aria-label="Release decision"
					>
						<div>
							<Label color={published || ready ? "green" : "grey"}>
								{published ? "Published" : ready ? "Validated" : "In progress"}
							</Label>
							<h2>{label}</h2>
							<p>
								{published
									? "Customer publication is confirmed. Review the release ticket and recorded FBCs below."
									: `${blockers.length} FBC preparation blockers. Shipment requires every planned validation job to pass on the selected candidate.`}
							</p>
							{candidateError && (
								<p>
									Candidate evidence could not be loaded. Open the workbench to
									retry.
								</p>
							)}
							<div className="rr-decision-actions">
								<Button variant="primary" onClick={() => select("workbench")}>
									{published ? "Inspect FBC record" : "Inspect validation"}
								</Button>
								<Button variant="secondary" onClick={() => select("issues")}>
									Browse JIRA issues
								</Button>
							</div>
						</div>
						<div className="rr-decision-coverage">
							<h3>
								{published
									? "Recorded validation evidence"
									: "Required validation coverage"}
							</h3>
							{validation.error ? (
								<LoadError
									title="Validation unavailable"
									onRetry={validation.refetch}
								>
									The required-job results could not be loaded.
								</LoadError>
							) : validation.data ? (
								<ValidationProgress data={validation.data} />
							) : (
								<Loading title="Loading validation…" />
							)}
							<p className="rr-muted">
								{published
									? "Historical evidence may be incomplete; this does not undo confirmed publication."
									: "Readiness also requires current sources and complete FBC preparation."}
							</p>
						</div>
					</section>
					<ol className="rr-release-flow" aria-label="Path to release">
						{[
							["ART build", !!candidate?.fbc_candidate, "Candidate FBCs built"],
							[
								"Konflux",
								candidate?.fbc_candidate?.source === "stage_snapshots",
								"Candidate staged",
							],
							["Validation", ready, "Full planned matrix"],
							[
								"Shipment",
								release.shipment_mr?.state === "merged",
								"Review and approval",
							],
							["Publication", published, "Customer availability"],
						].map(([name, done, note]) => (
							<li key={String(name)} className={done ? "done" : ""}>
								<strong>
									{done ? "✓ " : ""}
									{name}
								</strong>
								<span>{note}</span>
							</li>
						))}
					</ol>
					<section className="rr-record">
						<h2>Release coordination</h2>
						{facts}
					</section>
					{blockers.length > 0 && !published && (
						<section className="rr-record">
							<h2>What needs to happen next</h2>
							<ul>
								{blockers.map((b) => (
									<li key={`${b.code}-${b.operator}-${b.text}`}>
										<strong>{b.owner || "Release team"}</strong> · {b.text}
									</li>
								))}
							</ul>
						</section>
					)}
					{published && <FBCRecord candidate={candidate} />}
				</Tab>
				<Tab
					eventKey="workbench"
					title={<TabTitleText>Validation workbench</TabTitleText>}
				>
					<ValidationWorkbench
						data={validation.data}
						error={validation.error}
						retry={validation.refetch}
					/>
					{builds}
				</Tab>
				<Tab eventKey="issues" title={<TabTitleText>JIRA issues</TabTitleText>}>
					<p className="rr-muted rr-scope-note">
						Issues targeted to {formatReleaseName(release.name)}. Authored
						release notes will be added in a future update; ticket scope alone
						does not confirm a fix shipped.
					</p>
					{tickets}
				</Tab>
			</Tabs>
		</div>
	);
}

function ValidationWorkbench({
	data,
	error,
	retry,
}: {
	data?: Validation;
	error?: Error;
	retry: () => void;
}) {
	const [status, setStatus] = useState("all"),
		[arch, setArch] = useState("all");
	if (error)
		return (
			<LoadError title="Could not load validation" onRetry={retry}>
				Required-job evidence is unavailable.
			</LoadError>
		);
	if (!data) return <Loading title="Loading validation…" />;
	if (!data.plan_known)
		return (
			<Alert isInline variant="info" title="CI configuration unavailable">
				The job inventory has not been loaded. Readiness cannot be determined
				yet.
			</Alert>
		);
	if (data.total === 0)
		return (
			<Alert isInline variant="info" title="No CI configured">
				No periodic jobs are configured for this version in the latest imported
				job inventory. CI readiness cannot be established.
			</Alert>
		);
	const rows = (data.rows || []).filter(
		(r) =>
			(status === "all" ||
				(status === "unmatched"
					? r.state === "missing"
					: (r.ci_result || r.state) === status)) &&
			(arch === "all" || r.architecture === arch),
	);
	return (
		<section className="rr-record">
			<h2>Required validation matrix</h2>
			<ValidationProgress data={data} />
			<p className="rr-muted">
				Progress counts runs against the selected FBC. Results also show
				observed CI runs for other FBCs; those do not count toward shipment
				readiness.
			</p>
			<div className="rr-portfolio-tools">
				<label>
					CI result
					<select value={status} onChange={(e) => setStatus(e.target.value)}>
						<option value="all">All statuses</option>
						{["passed", "failed", "running", "missing", "unmatched"].map(
							(s) => (
								<option key={s} value={s}>
									{s === "missing"
										? "No CI result"
										: s === "unmatched"
											? "No selected-FBC run"
											: s}
								</option>
							),
						)}
					</select>
				</label>
				<label>
					Architecture
					<select value={arch} onChange={(e) => setArch(e.target.value)}>
						<option value="all">All architectures</option>
						{[
							...new Set(data.rows?.map((r) => r.architecture).filter(Boolean)),
						].map((a) => (
							<option value={a} key={a}>
								{a}
							</option>
						))}
					</select>
				</label>
				<Button
					variant="link"
					onClick={() => {
						setStatus("all");
						setArch("all");
					}}
				>
					Reset filters
				</Button>
			</div>
			<div className="rr-validation-table">
				<table>
					<caption className="pf-v6-screen-reader">
						Required jobs and latest verified candidate evidence
					</caption>
					<thead>
						<tr>
							<th scope="col">Configuration</th>
							<th scope="col">Platform</th>
							<th scope="col">Result</th>
							<th scope="col">Evidence</th>
						</tr>
					</thead>
					<tbody>
						{rows.map((row) => (
							<tr key={row.job}>
								<td>
									<a href={row.source_url} target="_blank" rel="noreferrer">
										{row.display_name || row.mode || row.job} ↗
									</a>
									<p className="rr-muted">
										{[row.cloud, row.storage].filter(Boolean).join(" / ")}
									</p>
								</td>
								<td>
									{row.ocp
										? `OCP ${row.ocp}`
										: row.mode === "Standalone"
											? "Standalone"
											: "Not specified"}
									<p className="rr-muted">{row.architecture}</p>
								</td>
								<td>
									<ValidationResult row={row} />
								</td>
								<td>
									{row.run_url ? (
										<a href={row.run_url} target="_blank" rel="noreferrer">
											{row.state === "missing"
												? "Open observed run ↗"
												: "Open Prow run ↗"}
										</a>
									) : (
										"No verified candidate run"
									)}
									<details>
										<summary>Details</summary>
										{row.evidence && <p className="rr-muted">{row.evidence}</p>}
										{row.tested_fbc && (
											<p className="rr-muted">
												Tested FBC: {row.tested_fbc.nvr}
												<br />
												<code>{row.tested_fbc.digest}</code>
											</p>
										)}
										{row.selected_fbc && (
											<p className="rr-muted">
												Selected FBC: {row.selected_fbc.nvr}
												<br />
												<code>{row.selected_fbc.digest}</code>
											</p>
										)}
										<code>{row.job}</code>
									</details>
								</td>
							</tr>
						))}
					</tbody>
				</table>
				{!rows.length && <p>No jobs match these filters.</p>}
			</div>
			<p className="rr-muted">
				Equivalent bundle runs and jobs without exact FBC identity do not
				satisfy this gate. Their available runs can still be inspected in CI
				history.
			</p>
		</section>
	);
}

function ValidationResult({ row }: { row: ValidationRow }) {
	const result = row.ci_result || row.state;
	const unmatched = row.state === "missing";
	if (result === "missing") {
		return <Label color="grey">No matching run for selected FBC</Label>;
	}
	const qualifier = unmatched
		? row.fbc_match === "newer"
			? "newer FBC"
			: row.fbc_match === "different"
				? "different FBC"
				: "FBC unverified"
		: "";
	return (
		<>
			<Label
				color={
					result === "failed"
						? "red"
						: result === "passed" && !unmatched
							? "green"
							: "blue"
				}
			>
				{result.charAt(0).toUpperCase() + result.slice(1)}
				{qualifier && ` · ${qualifier}`}
			</Label>
			{unmatched && (
				<p className="rr-muted">No matching run for selected FBC</p>
			)}
		</>
	);
}

function FBCRecord({ candidate }: { candidate?: ReleaseCandidate }) {
	const fbc = candidate?.fbc_candidate;
	return (
		<section className="rr-record">
			<h2>FBC record for this release</h2>
			{!fbc ? (
				<p>Historical FBC identities are not recorded for this release.</p>
			) : (
				<>
					<p className="rr-muted">
						{fbc.source === "stage_snapshots"
							? "FBCs recorded in the release's staged snapshots. The release ticket above provides coordination history."
							: "Stream fallback only: these FBCs are not verified as the catalogs published to customers. Historical shipment identity is incomplete."}
					</p>
					<div className="rr-validation-table">
						<table>
							<caption className="pf-v6-screen-reader">
								Recorded FBC build identities
							</caption>
							<thead>
								<tr>
									<th scope="col">Operator / OCP</th>
									<th scope="col">FBC build and digest</th>
								</tr>
							</thead>
							<tbody>
								{fbc.operators.flatMap((op) =>
									op.members.map((m) => (
										<tr key={`${op.operator}-${m.ocp}-${m.digest}`}>
											<td>
												{op.operator}
												<p className="rr-muted">OCP {m.ocp}</p>
											</td>
											<td>
												{m.build_url ? (
													<a
														href={m.build_url}
														target="_blank"
														rel="noreferrer"
													>
														{m.nvr || "ART build"} ↗
													</a>
												) : (
													m.nvr || "Build identity unresolved"
												)}
												<p>
													<code>{m.digest}</code>
												</p>
											</td>
										</tr>
									)),
								)}
							</tbody>
						</table>
					</div>
				</>
			)}
		</section>
	);
}
