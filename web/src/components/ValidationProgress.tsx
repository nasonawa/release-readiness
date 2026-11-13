import type { Validation } from "../api/types";

export default function ValidationProgress({ data }: { data?: Validation }) {
	if (!data?.plan_known)
		return (
			<div className="rr-validation-progress">
				<div className="rr-progress-track" />
				<p className="rr-muted">Required validation plan not configured</p>
			</div>
		);
	const values = [
		["passed", data.passed],
		["failed", data.failed],
		["running", data.running],
		["missing", data.missing],
	] as const;
	return (
		<div className="rr-validation-progress">
			<p>
				<strong>
					{data.passed} of {data.total}
				</strong>{" "}
				required jobs passed
			</p>
			<div
				className="rr-progress-track"
				role="img"
				aria-label={values.map(([label, n]) => `${n} ${label}`).join(", ")}
			>
				{values.map(([label, n]) => (
					<span
						key={label}
						className={label}
						style={{ width: `${data.total ? (n / data.total) * 100 : 0}%` }}
					/>
				))}
			</div>
			<p className="rr-muted">
				{data.failed} failed · {data.running} running · {data.missing} awaiting
				evidence
			</p>
			{!data.source_fresh && (
				<p className="rr-muted">Required sources are not up to date.</p>
			)}
		</div>
	);
}
