import * as preact from "preact";
import * as core from "vlens/core";
import * as rpc from "vlens/rpc";
import * as server from "@app/server";

type Job = server.Job;

type Data = { jobs: Job[] };

let gJobs: Job[] = [];
let gUrlInput = "";
let gSubmitting = false;
let gSubmitError = "";
let gActiveJobId = "";
let gPollInterval: ReturnType<typeof setInterval> | null = null;
let gHistoryOpen = false;

export async function fetch(_route: string, _prefix: string) {
    gPollInterval && clearInterval(gPollInterval);
    gPollInterval = null;
    gActiveJobId = "";
    gSubmitError = "";
    gSubmitting = false;
    gHistoryOpen = false;

    const [resp, err] = await server.ListJobs({});
    if (err) return rpc.err<Data>(err);
    gJobs = resp!.Jobs ?? [];

    // Resume polling if any job is still active
    const active = gJobs.find(j => j.Status === "queued" || j.Status === "running");
    if (active) {
        gActiveJobId = active.ID;
        startPolling();
    }

    return rpc.ok<Data>({ jobs: gJobs });
}

export function view(_route: string, _prefix: string, _data: Data): preact.ComponentChild {
    return (
        <div style={{ maxWidth: "680px", margin: "40px auto", padding: "0 16px", fontFamily: "sans-serif" }}>
            <h1 style={{ marginBottom: "24px" }}>Karaoke Maker</h1>
            {renderForm()}
            {renderActiveJob()}
            {renderHistory()}
        </div>
    );
}

function renderForm() {
    const activeJob = gJobs.find(j => j.ID === gActiveJobId);
    const busy = gSubmitting || (activeJob && (activeJob.Status === "queued" || activeJob.Status === "running"));

    return (
        <div style={{ display: "flex", gap: "8px", marginBottom: "24px" }}>
            <input
                type="text"
                placeholder="YouTube URL"
                value={gUrlInput}
                style={{ flex: 1, padding: "8px 12px", fontSize: "14px", border: "1px solid #ccc", borderRadius: "4px" }}
                onInput={(e) => {
                    gUrlInput = (e.target as HTMLInputElement).value;
                    core.scheduleRedraw();
                }}
                onKeyDown={(e) => { if (e.key === "Enter" && !busy) submitJob(); }}
            />
            <button
                onClick={() => { if (!busy) submitJob(); }}
                disabled={!!busy || !gUrlInput.trim()}
                style={{ padding: "8px 16px", fontSize: "14px", cursor: busy ? "default" : "pointer",
                         background: "#2563eb", color: "white", border: "none", borderRadius: "4px",
                         opacity: (busy || !gUrlInput.trim()) ? 0.5 : 1 }}
            >
                {gSubmitting ? "Submitting…" : "Make Karaoke"}
            </button>
            {gSubmitError && <span style={{ color: "red", alignSelf: "center", fontSize: "13px" }}>{gSubmitError}</span>}
        </div>
    );
}

function renderActiveJob() {
    if (!gActiveJobId) return null;
    const job = gJobs.find(j => j.ID === gActiveJobId);
    if (!job || job.Status === "done" || job.Status === "error") return null;

    const elapsed = job.Step === "separating" ? formatElapsed(job.StepStartedAt) : "";
    const label = job.Status === "queued"
        ? "Queued…"
        : job.Step === "downloading"
            ? "Downloading audio…"
            : job.Step === "separating"
                ? `Separating stems${job.Title ? ` for "${job.Title}"` : ""}…`
                : "Processing…";

    return (
        <div style={{ padding: "12px 16px", background: "#f0f9ff", border: "1px solid #bae6fd",
                      borderRadius: "6px", marginBottom: "24px" }}>
            <div style={{ display: "flex", alignItems: "center", gap: "10px",
                          marginBottom: job.Progress > 0 ? "8px" : "0" }}>
                <Spinner />
                <span style={{ fontSize: "14px" }}>{label}{elapsed && <span style={{ color: "#6b7280", marginLeft: "8px" }}>{elapsed}</span>}</span>
            </div>
            {job.Progress > 0 && (
                <div style={{ height: "4px", background: "#e0f2fe", borderRadius: "2px", overflow: "hidden" }}>
                    <div style={{ height: "100%", width: `${job.Progress}%`, background: "#2563eb",
                                  borderRadius: "2px", transition: "width 0.5s ease-in-out" }} />
                </div>
            )}
        </div>
    );
}

function renderHistory() {
    const historyJobs = gJobs.filter(j => j.Status === "done" || j.Status === "error");
    if (historyJobs.length === 0) return null;

    return (
        <div>
            <button
                onClick={() => { gHistoryOpen = !gHistoryOpen; core.scheduleRedraw(); }}
                style={{ display: "flex", alignItems: "center", gap: "6px", background: "none",
                         border: "none", padding: "0", cursor: "pointer", marginBottom: "12px" }}
            >
                <span style={{ fontSize: "16px", fontWeight: 600 }}>History</span>
                <span style={{ fontSize: "13px", color: "#6b7280" }}>({historyJobs.length})</span>
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="#6b7280"
                     strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"
                     style={{ transform: gHistoryOpen ? "rotate(180deg)" : "rotate(0deg)", transition: "transform 0.15s" }}>
                    <polyline points="6 9 12 15 18 9" />
                </svg>
            </button>
            {gHistoryOpen && (
                <div style={{ display: "flex", flexDirection: "column", gap: "12px" }}>
                    {historyJobs.map(job => renderJobCard(job))}
                </div>
            )}
        </div>
    );
}

function renderJobCard(job: Job) {
    const isActive = job.ID === gActiveJobId;

    return (
        <div key={job.ID} style={{ padding: "14px 16px", border: "1px solid #e5e7eb", borderRadius: "6px",
                                    background: isActive ? "#fafafa" : "white" }}>
            <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: "8px" }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontSize: "14px", fontWeight: 500, whiteSpace: "nowrap",
                                  overflow: "hidden", textOverflow: "ellipsis" }}>
                        {job.Title || job.URL}
                    </div>
                    <div style={{ fontSize: "12px", color: "#6b7280", marginTop: "2px" }}>
                        {new Date(job.CreatedAt).toLocaleString()}
                    </div>
                </div>
                <StatusBadge status={job.Status} />
            </div>

            {job.Status === "error" && (
                <div style={{ marginTop: "8px", padding: "8px", background: "#fef2f2",
                               borderRadius: "4px", fontSize: "13px", color: "#dc2626" }}>
                    {job.Error}
                </div>
            )}

            {job.Status === "done" && (
                <div style={{ marginTop: "10px" }}>
                    <audio controls src={`/jobs/${job.ID}/no_vocals.wav`}
                           style={{ width: "100%", marginBottom: "8px" }} />
                    <div style={{ display: "flex", gap: "8px" }}>
                        <a href={`/jobs/${job.ID}/no_vocals.wav`} download
                           style={downloadLinkStyle}>
                            ↓ Karaoke (no vocals)
                        </a>
                        <a href={`/jobs/${job.ID}/vocals.wav`} download
                           style={downloadLinkStyle}>
                            ↓ Vocals only
                        </a>
                    </div>
                </div>
            )}
        </div>
    );
}

const downloadLinkStyle: preact.JSX.CSSProperties = {
    display: "inline-block",
    padding: "5px 10px",
    fontSize: "13px",
    background: "#f3f4f6",
    border: "1px solid #d1d5db",
    borderRadius: "4px",
    color: "#111827",
    textDecoration: "none",
};

function StatusBadge({ status }: { status: string }) {
    const colors: Record<string, { bg: string; text: string }> = {
        queued:  { bg: "#fef9c3", text: "#854d0e" },
        running: { bg: "#dbeafe", text: "#1e40af" },
        done:    { bg: "#dcfce7", text: "#166534" },
        error:   { bg: "#fee2e2", text: "#991b1b" },
    };
    const c = colors[status] ?? { bg: "#f3f4f6", text: "#374151" };
    return (
        <span style={{ padding: "2px 8px", borderRadius: "9999px", fontSize: "12px",
                        background: c.bg, color: c.text, whiteSpace: "nowrap" }}>
            {status}
        </span>
    );
}

function formatElapsed(startISO: string): string {
    if (!startISO || startISO === "0001-01-01T00:00:00Z") return "";
    const secs = Math.floor((Date.now() - new Date(startISO).getTime()) / 1000);
    if (secs <= 0) return "";
    const m = Math.floor(secs / 60);
    const s = secs % 60;
    return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function Spinner() {
    return (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
             strokeWidth="2" style={{ animation: "spin 1s linear infinite", color: "#2563eb" }}>
            <style>{`@keyframes spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }`}</style>
            <circle cx="12" cy="12" r="10" strokeOpacity="0.25" />
            <path d="M12 2a10 10 0 0 1 10 10" />
        </svg>
    );
}

async function submitJob() {
    const url = gUrlInput.trim();
    if (!url || gSubmitting) return;

    gSubmitting = true;
    gSubmitError = "";
    core.scheduleRedraw();

    const [resp, err] = await server.SubmitJob({ URL: url });
    gSubmitting = false;

    if (err || !resp) {
        gSubmitError = err || "unknown error";
        core.scheduleRedraw();
        return;
    }

    gUrlInput = "";
    gActiveJobId = resp.JobID;

    // Optimistically add the job to the list
    const newJob: Job = {
        ID: resp.JobID,
        URL: url,
        Status: "queued",
        Step: "",
        Progress: 0,
        Title: "",
        CreatedAt: new Date().toISOString(),
        CompletedAt: new Date().toISOString(),
        Error: "",
        StepStartedAt: "",
    };
    gJobs.unshift(newJob);

    startPolling();
    core.scheduleRedraw();
}

function startPolling() {
    if (gPollInterval !== null) clearInterval(gPollInterval);
    gPollInterval = setInterval(pollActiveJob, 3000);
}

async function pollActiveJob() {
    if (!gActiveJobId) {
        stopPolling();
        return;
    }

    const [job, err] = await server.GetJob({ JobID: gActiveJobId });
    if (err || !job) return;

    const idx = gJobs.findIndex(j => j.ID === job.ID);
    if (idx >= 0) {
        gJobs[idx] = job;
    } else {
        gJobs.unshift(job);
    }

    if (job.Status === "done" || job.Status === "error") {
        stopPolling();
    }

    core.scheduleRedraw();
}

function stopPolling() {
    if (gPollInterval !== null) {
        clearInterval(gPollInterval);
        gPollInterval = null;
    }
}
