import {
	Alert,
	Button,
	Checkbox,
	EmptyState,
	EmptyStateBody,
	Label,
	PageSection,
	Switch,
	TextInput,
	Title,
	ToggleGroup,
	ToggleGroupItem,
} from "@patternfly/react-core";
import { ColumnsIcon, ThLargeIcon } from "@patternfly/react-icons";
import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { listReleasesOverview } from "../api/client";
import type { ReleaseOverview } from "../api/types";
import LoadError, { Loading } from "../components/LoadError";
import ValidationProgress from "../components/ValidationProgress";
import { useCachedFetch } from "../hooks/useCachedFetch";
import { useConfig } from "../hooks/useConfig";
import { formatReleaseName, jiraIssueUrl } from "../utils/links";
import {
	groupStreams,
	portfolioLabel,
	releaseStage,
	stageReleases,
	streamIdentity,
} from "../utils/portfolio";

const stages = [
	{
		key: "validation",
		title: "Validation",
		description:
			"Build preparation, testing, or release evidence still needs attention.",
	},
	{
		key: "shipment",
		title: "Shipment preparation",
		description:
			"Required validation passed. Prepare and approve the shipment.",
	},
	{
		key: "available",
		title: "Available to customers",
		description: "Publication confirmed by the Red Hat container catalog.",
	},
] as const;

export default function ReleasePortfolio() {
	const { data, error, refetch } = useCachedFetch(
		"portfolio",
		listReleasesOverview,
	);
	const config = useConfig();
	const [params, setParams] = useSearchParams();
	const layout = params.get("layout") === "stages" ? "stages" : "cards";
	const product = params.get("product") || "all",
		query = params.get("q") || "";
	const history = params.get("history") === "1";
	const available = params.get("available") === "1";
	const host = useRef<HTMLDivElement>(null);
	const before = useRef(new Map<string, DOMRect>());
	const animations = useRef<Animation[]>([]);
	const stopMotion = useCallback(() => {
		animations.current.forEach((a) => {
			a.cancel();
		});
		animations.current = [];
	}, []);
	function change(key: string, value: string) {
		setParams((prev) => {
			const next = new URLSearchParams(prev);
			if (value) next.set(key, value);
			else next.delete(key);
			return next;
		});
	}
	function switchLayout(next: string) {
		if (next === layout) return;
		before.current.clear();
		host.current
			?.querySelectorAll<HTMLElement>("[data-release]")
			.forEach((node) => {
				const rect = node.getBoundingClientRect();
				before.current.set(node.dataset.release!, rect);
				if (!before.current.has(node.dataset.stream!))
					before.current.set(node.dataset.stream!, rect);
			});
		stopMotion();
		change("layout", next);
	}
	useLayoutEffect(() => {
		if (!before.current.size || !host.current) return;
		const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
		if (!reduced) {
			host.current
				.querySelectorAll<HTMLElement>("[data-release]")
				.forEach((node) => {
					const from =
						before.current.get(node.dataset.release!) ||
						before.current.get(node.dataset.stream!);
					if (!from) return;
					const to = node.getBoundingClientRect();
					const a = node.animate(
						[
							{
								transform: `translate(${from.x - to.x}px,${from.y - to.y}px) scale(${from.width / to.width},${from.height / to.height})`,
								transformOrigin: "top left",
								zIndex: 2,
							},
							{ transform: "none", transformOrigin: "top left", zIndex: 2 },
						],
						{ duration: 560, easing: "cubic-bezier(.22,1,.36,1)" },
					);
					animations.current.push(a);
				});
			if (layout === "stages")
				host.current
					.querySelectorAll(".rr-stage-heading,.rr-stage-empty")
					.forEach((node) => {
						animations.current.push(
							node.animate([{ opacity: 0 }, { opacity: 1 }], {
								duration: 300,
								delay: 140,
								fill: "backwards",
							}),
						);
					});
		}
		before.current.clear();
	}, [layout]);
	useEffect(() => {
		const media = matchMedia("(prefers-reduced-motion: reduce)");
		window.addEventListener("resize", stopMotion);
		media.addEventListener("change", stopMotion);
		return () => {
			stopMotion();
			window.removeEventListener("resize", stopMotion);
			media.removeEventListener("change", stopMotion);
		};
	}, [stopMotion]);
	const path = (ov: ReleaseOverview) =>
		`/releases/${encodeURIComponent(ov.release.name)}?${new URLSearchParams({ layout, product, ...(history ? { history: "1" } : {}), ...(available ? { available: "1" } : {}), ...(query ? { q: query } : {}) })}`;
	const ticket = (ov: ReleaseOverview) =>
		ov.release.release_ticket_key ? (
			<a
				href={jiraIssueUrl(
					ov.release.release_ticket_key,
					config?.jira_base_url || "https://redhat.atlassian.net",
				)}
				target="_blank"
				rel="noreferrer"
			>
				{ov.release.release_ticket_key} ↗
			</a>
		) : (
			<span className="rr-muted">No release ticket</span>
		);
	function facts(ov: ReleaseOverview) {
		return (
			<>
				{ov.published ? (
					<p className="rr-muted">
						Published{" "}
						{ov.shipped_at
							? new Date(ov.shipped_at).toLocaleDateString()
							: "date unavailable"}
					</p>
				) : (
					<ValidationProgress data={ov.portfolio?.validation} />
				)}
				{ov.published &&
					ov.portfolio?.validation.plan_known &&
					ov.portfolio.validation.total === 0 && (
						<p className="rr-muted">No CI configured</p>
					)}
				<div className="rr-card-facts">
					<span>{ticket(ov)}</span>
					<span>{ov.release.release_ticket_assignee || "Unassigned"}</span>
				</div>
				{!ov.published && (
					<p className="rr-muted">
						Target:{" "}
						{ov.release.due_date || ov.release.release_date
							? new Date(
									ov.release.due_date || ov.release.release_date!,
								).toLocaleDateString(undefined, {
									year: "numeric",
									month: "short",
									day: "numeric",
									timeZone: "UTC",
								})
							: "Not scheduled"}
					</p>
				)}
				<Link className="rr-card-action" to={path(ov)}>
					{ov.published ? "View release & FBC record" : "View release briefing"}{" "}
					→
				</Link>
			</>
		);
	}
	if (!data || error)
		return (
			<PageSection>
				{error ? (
					<LoadError
						title="Could not load the release portfolio"
						onRetry={refetch}
					>
						Release data is unavailable. Retry the request.
					</LoadError>
				) : (
					<Loading title="Loading release portfolio…" />
				)}
			</PageSection>
		);
	const products = [
		...new Set(data.map((ov) => streamIdentity(ov.release.name).product)),
	].sort();
	const filtered = data.filter(
		(ov) =>
			(product === "all" ||
				streamIdentity(ov.release.name).product === product) &&
			`${ov.release.name} ${ov.release.release_ticket_key || ""}`
				.toLowerCase()
				.includes(query.toLowerCase()),
	);
	const allGroups = groupStreams(filtered);
	const groups =
		layout === "cards" && !available
			? allGroups.filter((g) => g.releases.some((r) => !r.published))
			: allGroups;
	const staged = stageReleases(filtered, history);
	return (
		<PageSection className="rr-page">
			<div className="rr-portfolio-heading">
				<div>
					<Title headingLevel="h1" size="3xl">
						Release portfolio
					</Title>
					<p className="rr-muted">
						{layout === "cards"
							? "Follow each product stream and its next release."
							: "Follow individual releases from validation to customer availability."}
					</p>
				</div>
				<ToggleGroup aria-label="Release portfolio view">
					<ToggleGroupItem
						text="Cards"
						icon={<ThLargeIcon />}
						isSelected={layout === "cards"}
						onChange={() => switchLayout("cards")}
					/>
					<ToggleGroupItem
						text="Stages"
						icon={<ColumnsIcon />}
						isSelected={layout === "stages"}
						onChange={() => switchLayout("stages")}
					/>
				</ToggleGroup>
			</div>
			{error && (
				<Alert
					variant="warning"
					isInline
					title="Showing previously loaded releases"
				>
					<Button variant="link" onClick={refetch}>
						Retry refresh
					</Button>
				</Alert>
			)}
			<div className="rr-portfolio-summary">
				<div>
					<strong>{groups.length}</strong>
					<span>Product streams</span>
				</div>
				{stages.map((s) => (
					<div key={s.key}>
						<strong>
							{staged.filter((ov) => releaseStage(ov) === s.key).length}
						</strong>
						<span>{s.title}</span>
					</div>
				))}
			</div>
			<div className="rr-portfolio-tools">
				<label>
					Product
					<select
						value={product}
						onChange={(e) => change("product", e.target.value)}
					>
						<option value="all">All products</option>
						{products.map((p) => (
							<option key={p} value={p}>
								{p === "quay" ? "Quay" : p.toUpperCase()}
							</option>
						))}
					</select>
				</label>
				<TextInput
					aria-label="Find a release or ticket"
					placeholder="Find a release or ticket"
					value={query}
					onChange={(_, v) => change("q", v)}
				/>
				{layout === "cards" && (
					<Checkbox
						id="show-available-releases"
						label="Show available releases"
						isChecked={available}
						onChange={(_, checked) => change("available", checked ? "1" : "")}
					/>
				)}
				{layout === "stages" && (
					<Switch
						id="release-history"
						label="Show history"
						isChecked={history}
						onChange={(_, v) => change("history", v ? "1" : "")}
					/>
				)}
			</div>
			<p className="rr-muted rr-scope-note">
				{layout === "cards"
					? "Cards group x.y streams. Open a z-stream to see its exact builds, validation and issues."
					: history
						? "Showing all recorded z-streams, including earlier releases."
						: "Showing upcoming releases and the latest published z-stream in each stream. Enable history for earlier releases."}
			</p>
			{!(layout === "cards" ? groups.length : staged.length) && (
				<EmptyState
					titleText={data.length ? "No matching releases" : "No releases yet"}
					headingLevel="h2"
				>
					<EmptyStateBody>
						{data.length
							? layout === "cards" && !available
								? "Try another search or enable Show available releases."
								: "Try another product, version or ticket."
							: "Releases appear when JIRA sync discovers them. Source status is available in the header."}
					</EmptyStateBody>
				</EmptyState>
			)}
			<div ref={host} className="rr-portfolio-content">
				{layout === "cards" ? (
					<div className="rr-card-grid">
						{groups.map((g) => (
							<article
								className="rr-release-card"
								key={g.key}
								data-release={g.featured.release.name}
								data-stream={g.key}
							>
								<div className="rr-card-heading">
									<h2>
										<Link to={path(g.featured)}>
											{g.product === "quay" ? "Quay" : g.product.toUpperCase()}{" "}
											{g.version}
										</Link>
									</h2>
									<Label
										color={
											g.featured.published
												? "green"
												: g.featured.portfolio?.validation.ready
													? "blue"
													: "grey"
										}
									>
										{portfolioLabel(g.featured)}
									</Label>
								</div>
								<p className="rr-card-target">
									{g.next ? "Next release" : "Latest release"}{" "}
									<strong>{formatReleaseName(g.featured.release.name)}</strong>
								</p>
								{facts(g.featured)}
								{g.latest && g.latest !== g.featured && (
									<p className="rr-latest">
										Available:{" "}
										<Link to={path(g.latest)}>
											{formatReleaseName(g.latest.release.name)}
										</Link>
									</p>
								)}
								<details className="rr-stream-history">
									<summary>Z-stream details ({g.releases.length})</summary>
									<ul>
										{g.releases.map((ov) => (
											<li key={ov.release.name}>
												<Link to={path(ov)}>
													{formatReleaseName(ov.release.name)}
												</Link>
												<span className="rr-muted">
													{ov.published
														? "Published"
														: ov.next_in_stream
															? "Next release"
															: ov.release.released
																? "Marked released"
																: "Planned"}
												</span>
												{ticket(ov)}
											</li>
										))}
									</ul>
								</details>
							</article>
						))}
					</div>
				) : (
					<div className="rr-stage-grid">
						{stages.map((s, i) => (
							<section
								key={s.key}
								className={`rr-stage-lane ${s.key}`}
								aria-labelledby={`stage-${s.key}`}
							>
								<header className="rr-stage-heading">
									<div className="rr-card-heading">
										<span>Stage {i + 1}</span>
										<span>
											{staged.filter((ov) => releaseStage(ov) === s.key).length}{" "}
											releases
										</span>
									</div>
									<h2 id={`stage-${s.key}`}>{s.title}</h2>
									<p>{s.description}</p>
								</header>
								<div className="rr-stage-items">
									{staged
										.filter((ov) => releaseStage(ov) === s.key)
										.map((ov) => (
											<article
												className="rr-release-card"
												key={ov.release.name}
												data-release={ov.release.name}
												data-stream={streamIdentity(ov.release.name).key}
											>
												<div className="rr-card-heading">
													<h3>
														<Link to={path(ov)}>
															{formatReleaseName(ov.release.name)}
														</Link>
													</h3>
													<Label color={ov.published ? "green" : "grey"}>
														{portfolioLabel(ov)}
													</Label>
												</div>
												{facts(ov)}
											</article>
										))}
									{!staged.some((ov) => releaseStage(ov) === s.key) && (
										<div className="rr-stage-empty">
											No releases in this stage.
										</div>
									)}
								</div>
							</section>
						))}
					</div>
				)}
			</div>
			<p className="pf-v6-screen-reader" role="status">
				{layout === "cards"
					? `${groups.length} product stream cards`
					: `${staged.length} z-stream releases grouped by stage`}
			</p>
		</PageSection>
	);
}
