package flightreview

// reportHTML is the whole report: semantic HTML with inline CSS, light mode,
// no scripts, no external stylesheets, fonts, or images, so it stays readable
// with no daemon, workspace, or network. html/template escapes every value.
const reportHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} {{.Range}}</title>
<style>
:root { color-scheme: light; }
body { margin: 0; background: #ffffff; color: #1c2430; font: 14px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
main { max-width: 1180px; margin: 0 auto; padding: 24px 28px 64px; }
h1 { font-size: 24px; margin: 0 0 4px; }
h2 { font-size: 18px; margin: 0 0 8px; padding-top: 8px; border-top: 2px solid #1c2430; }
h3 { font-size: 14px; margin: 18px 0 6px; }
section { margin-top: 36px; }
p, li { max-width: 78ch; }
.meta { color: #55606e; margin: 0 0 12px; }
.note { color: #55606e; font-size: 13px; }
table { border-collapse: collapse; width: 100%; margin: 8px 0 16px; font-size: 13px; }
th, td { border: 1px solid #c9d0d9; padding: 5px 8px; text-align: left; vertical-align: top; }
th { background: #eef1f5; font-weight: 600; }
td.excerpt { white-space: pre-wrap; word-break: break-word; max-width: 38ch; }
td.evidence, .evidence { font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 12px; color: #3d4756; word-break: break-all; }
.aggregate { font-weight: 600; }
.pending { background: #fff3cd; padding: 0 4px; }
.bad { color: #9a1c1c; }
.empty { color: #55606e; font-style: italic; }
dl { margin: 6px 0 14px; }
dt { font-weight: 600; margin-top: 6px; }
dd { margin: 0 0 0 16px; }
.candidate { border: 1px solid #c9d0d9; border-left: 4px solid #1c2430; padding: 8px 14px; margin: 10px 0; }
.options { color: #55606e; }
@media print {
  body { -webkit-print-color-adjust: exact; print-color-adjust: exact; }
  section { break-inside: avoid-page; }
}
</style>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p class="meta">{{.Range}} · {{.CohortN}} flights · {{.CohortRule}}</p>
{{if .RefreshErrors}}<p class="bad">Some evidence could not be refreshed before this report was rendered:</p>
<ul>{{range .RefreshErrors}}<li class="bad">{{.}}</li>{{end}}</ul>{{end}}

<section id="data-quality">
<h2>1. Data quality and coverage</h2>
<p class="note">Read this first. Every figure below states how many flights it counts, how many had the evidence it needs, and how many the cohort holds; flights without the evidence are named here, never counted as zero.</p>
<h3>Evidence coverage</h3>
<table>
<thead><tr><th scope="col">Coverage</th><th scope="col">Flights</th></tr></thead>
<tbody>{{range .Coverage}}<tr><th scope="row">{{.Name}}</th><td>{{.N}}</td></tr>{{end}}</tbody>
</table>
<h3>Gaps and failures</h3>
<table>
<thead><tr><th scope="col">Finding</th><th scope="col">Count</th></tr></thead>
<tbody>{{range .Quality}}<tr><th scope="row">{{.Label}}</th><td>{{with .Ratio}}{{template "ratio" .}}{{end}}{{if and .Ratio .Value}} · {{end}}{{.Value}}</td></tr>{{end}}</tbody>
</table>
</section>

<section id="cohort">
<h2>2. Cohort</h2>
<p class="note">Active flights are censored in duration metrics: they are not failures, and their open phase has no duration. Orphaned flights are a separate status flag, not an outcome.</p>
<h3>Aggregates</h3>
<table>
<thead><tr><th scope="col">Measure</th><th scope="col">Flights</th></tr></thead>
<tbody>{{range .Aggregates}}<tr><th scope="row">{{.Label}}</th><td>{{template "ratio" .}}</td></tr>{{end}}</tbody>
</table>
<h3>Outcome funnel</h3>
<table>
<thead><tr><th scope="col">Outcome</th><th scope="col">Flights</th></tr></thead>
<tbody>{{range .Funnel}}<tr><th scope="row">{{.Name}}</th><td>{{.N}}</td></tr>{{end}}</tbody>
</table>
<h3>Time in each phase (p50 / p90, known durations only)</h3>
<table>
<thead><tr><th scope="col">Phase</th><th scope="col">p50</th><th scope="col">p90</th><th scope="col">Flights with a known duration</th></tr></thead>
<tbody>{{range .Durations}}<tr><th scope="row">{{.Phase}}</th><td>{{.P50}}</td><td>{{.P90}}</td><td>{{template "ratio" .Ratio}}</td></tr>{{end}}</tbody>
</table>
<h3>Flights</h3>
{{if .Rows}}<table>
<thead><tr><th scope="col">Flight</th><th scope="col">Outcome</th><th scope="col">Coverage</th><th scope="col">Duration</th><th scope="col">Phase path</th><th scope="col">Interventions</th><th scope="col">Waiting</th><th scope="col">Reviewers</th><th scope="col">Failures</th></tr></thead>
<tbody>{{range .Rows}}<tr>
<th scope="row">{{.ID}}<br><span class="note">{{.Name}}</span></th>
<td>{{.Outcome}}{{if .Orphaned}} · <strong>orphaned_active</strong>{{end}}</td>
<td>{{.Coverage}}</td><td>{{.Duration}}</td><td>{{.PhasePath}}</td>
<td>{{.Interventions}}</td><td>{{.Waiting}}</td><td>{{.Reviewers}}</td><td>{{.Failures}}</td>
</tr>{{end}}</tbody>
</table>{{else}}<p class="empty">No flights match this cohort.</p>{{end}}
</section>

<section id="orphans">
<h2>3. Orphan inventory</h2>
<p class="note">Active flights whose workspace or every current session is gone. Settle these before reading the rest as a measure of the workflow.</p>
{{if .Orphans}}<table>
<thead><tr><th scope="col">Flight</th><th scope="col">Workspace / branch</th><th scope="col">Branch and PR evidence</th><th scope="col">Outcome</th><th scope="col">Proposed action</th></tr></thead>
<tbody>{{range .Orphans}}<tr>
<th scope="row">{{.ID}}<br><span class="note">{{.Name}}</span></th>
<td>{{.Workspace}}<br>{{.Branch}}</td><td>{{.Evidence}}</td>
<td>{{.Outcome}}</td><td>{{.Action}}</td>
</tr>{{end}}</tbody>
</table>{{else}}<p class="empty">No orphaned flights in this cohort.</p>{{end}}
</section>

<section id="interventions">
<h2>4. Intervention ledger</h2>
<p class="note">Every counted user action after takeoff, verbatim and local to this file. Pilot guidance delivered into a transcript is not listed. Evidence IDs name the event, operation, reconciliation record, or chat record behind each row.</p>
{{if .Interventions}}<table>
<thead><tr><th scope="col">Flight</th><th scope="col">When</th><th scope="col">Since takeoff</th><th scope="col">Phase</th><th scope="col">Kind</th><th scope="col">Mechanism</th><th scope="col">Chat label</th><th scope="col">Session</th><th scope="col">Text</th><th scope="col">Evidence</th></tr></thead>
<tbody>{{range .Interventions}}<tr>
<td>{{.Flight}}</td><td>{{.At}}</td><td>{{.Elapsed}}</td><td>{{.Phase}}</td><td>{{.Kind}}</td><td>{{.Classification}}</td><td>{{.Label}}</td><td>{{.Who}}</td>
<td class="excerpt">{{.Excerpt}}</td><td class="evidence">{{.Evidence}}</td>
</tr>{{end}}</tbody>
</table>{{else}}<p class="empty">No counted interventions in this cohort.</p>{{end}}
</section>

<section id="bottlenecks">
<h2>5. Bottlenecks</h2>
<p class="note">Grouped by Jev label for chat and by action otherwise, ranked by affected flights. Only flights with complete intervention evidence are counted; user time is not measured.</p>
{{if .Bottlenecks}}<table>
<thead><tr><th scope="col">Group</th><th scope="col">Affected flights</th><th scope="col">Interventions</th><th scope="col">Representative text</th></tr></thead>
<tbody>{{range .Bottlenecks}}<tr>
<th scope="row">{{.Title}}</th><td>{{template "ratio" .Affected}}</td><td>{{.Interventions}}</td>
<td class="excerpt">{{range .Representative}}{{.}}
{{end}}</td>
</tr>{{end}}</tbody>
</table>{{else}}<p class="empty">No interventions to group.</p>{{end}}
</section>

<section id="candidates">
<h2>6. Product candidates</h2>
<p class="note">A candidate needs two affected flights, or one flight plus a mechanism you confirmed. The fields below are starting points for the meeting, not findings; the evidence is the repeated pattern and its IDs.</p>
{{range .Candidates}}<div class="candidate">
<h3>{{.ID}} · {{.Title}}</h3>
<p>Affected flights: {{template "ratio" .Affected}}{{if .Confirmed}} · mechanism confirmed by you{{end}}</p>
<p class="evidence">{{range .Evidence}}{{.}} {{end}}</p>
<dl>
<dt>Smallest experiment</dt><dd>{{.Experiment}}</dd>
<dt>Intervention removed</dt><dd>{{.Removes}}</dd>
<dt>New failure it could introduce</dt><dd>{{.NewFailure}}</dd>
<dt>Counter-metric</dt><dd>{{.CounterMetric}}</dd>
</dl>
</div>{{else}}<p class="empty">No pattern repeats across two flights yet.</p>{{end}}
</section>

<section id="exceptions">
<h2>7. Exceptions and evidence gaps</h2>
<p class="note">One-flight findings and missing evidence, kept apart from recurring patterns.</p>
{{if .Exceptions}}<table>
<thead><tr><th scope="col">Flight</th><th scope="col">Finding</th><th scope="col">Evidence</th></tr></thead>
<tbody>{{range .Exceptions}}<tr><td>{{.Subject}}</td><td>{{.Finding}}</td><td class="evidence">{{.Evidence}}</td></tr>{{end}}</tbody>
</table>{{else}}<p class="empty">No exceptions or gaps.</p>{{end}}
</section>

<section id="agenda">
<h2>8. Decision agenda</h2>
<p class="note">Decide in this order: data quality, orphans, exceptions, then one or two experiments with their success metric. Decisions are recorded in the decisions file, never in this report.</p>
{{if .Agenda}}<ol>
{{range .Agenda}}<li>
<strong>{{.ID}}</strong>{{if .Title}} · {{.Title}}{{end}}
{{if .Status}}<span class="pending">carried forward · left as {{.Status}}</span><br>{{.Detail}}{{end}}
<br><span class="options">Decision: Accepted · Rejected · Needs data · Deferred</span>
</li>{{end}}
</ol>{{else}}<p class="empty">Nothing to decide.</p>{{end}}
</section>
</main>
</body>
</html>
{{define "ratio"}}<span class="aggregate" data-name="{{.Name}}" data-numerator="{{.Num}}" data-covered="{{.Covered}}" data-cohort="{{.Cohort}}">{{.Text}}</span>{{end}}
`
