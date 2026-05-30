import * as preact from "preact";
import * as core from "vlens/core";
import * as rpc from "vlens/rpc";
import * as server from "@app/server";

const nativeFetch = window.fetch.bind(window);

type Job = server.Job;

type Data = { jobs: Job[] };

let gJobs: Job[] = [];
let gUrlInput = "";
let gPitchShift = 0;
let gSpeedAdjust = 1.0;
let gSubmitting = false;
let gSubmitError = "";
let gActiveJobId = "";
let gEventSource: EventSource | null = null;
let gHistoryOpen = false;
let gExpandedErrors = new Set<string>();
let gExpandedLyrics = new Set<string>();
let gDeletingIds = new Set<string>();
let gInputMode: "url" | "upload" = "url";
let gUploadFile: File | null = null;

export async function fetch(_route: string, _prefix: string) {
    stopSSE();
    gActiveJobId = "";
    gSubmitError = "";
    gSubmitting = false;
    gPitchShift = 0;
    gSpeedAdjust = 1.0;
    gHistoryOpen = false;
    gExpandedLyrics.clear();
    gDeletingIds.clear();
    gInputMode = "url";
    gUploadFile = null;

    const [resp, err] = await server.ListJobs({});
    if (err) return rpc.err<Data>(err);
    gJobs = resp!.Jobs ?? [];

    // Resume SSE stream if any job is still active
    const active = gJobs.find(j => j.Status === "queued" || j.Status === "running");
    if (active) {
        gActiveJobId = active.ID;
        startSSE(active.ID);
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

const SEMITONE_OPTIONS = [-6, -5, -4, -3, -2, -1, 0, 1, 2, 3, 4, 5, 6];
const SPEED_OPTIONS = [0.5, 0.6, 0.75, 0.85, 0.9, 1.0, 1.1, 1.25, 1.5, 2.0];

function semitonelabel(n: number): string {
    if (n === 0) return "0 (original)";
    return (n > 0 ? `+${n}` : `${n}`) + " st";
}

function speedLabel(s: number): string {
    if (s === 1.0) return "1× (original)";
    return s + "×";
}

function renderForm() {
    const activeJob = gJobs.find(j => j.ID === gActiveJobId);
    const busy = gSubmitting || (activeJob && (activeJob.Status === "queued" || activeJob.Status === "running"));
    const canSubmit = !busy && (gInputMode === "url" ? !!gUrlInput.trim() : !!gUploadFile);

    const tabStyle = (active: boolean): preact.JSX.CSSProperties => ({
        padding: "6px 14px", fontSize: "13px", border: "1px solid #d1d5db",
        borderRadius: "4px", cursor: busy ? "default" : "pointer",
        background: active ? "#2563eb" : "white",
        color: active ? "white" : "#374151",
        fontWeight: active ? 600 : 400,
    });

    return (
        <div style={{ marginBottom: "24px" }}>
            <div style={{ display: "flex", gap: "6px", marginBottom: "10px" }}>
                <button style={tabStyle(gInputMode === "url")} disabled={!!busy}
                    onClick={() => { gInputMode = "url"; gSubmitError = ""; core.scheduleRedraw(); }}>
                    YouTube URL
                </button>
                <button style={tabStyle(gInputMode === "upload")} disabled={!!busy}
                    onClick={() => { gInputMode = "upload"; gSubmitError = ""; core.scheduleRedraw(); }}>
                    Upload File
                </button>
            </div>

            <div style={{ display: "flex", gap: "8px", marginBottom: "8px" }}>
                {gInputMode === "url" ? (
                    <input
                        type="text"
                        placeholder="YouTube URL"
                        value={gUrlInput}
                        style={{ flex: 1, padding: "8px 12px", fontSize: "14px", border: "1px solid #ccc", borderRadius: "4px" }}
                        onInput={(e) => {
                            gUrlInput = (e.target as HTMLInputElement).value;
                            core.scheduleRedraw();
                        }}
                        onKeyDown={(e) => { if (e.key === "Enter" && canSubmit) submitJob(); }}
                    />
                ) : (
                    <label style={{ flex: 1, display: "flex", alignItems: "center", gap: "10px",
                                    padding: "8px 12px", border: "1px dashed #9ca3af", borderRadius: "4px",
                                    cursor: busy ? "default" : "pointer", background: "#f9fafb" }}>
                        <input
                            type="file"
                            accept=".mp3,.wav,.flac,.ogg,.m4a,.aac,.opus"
                            style={{ display: "none" }}
                            onChange={(e) => {
                                gUploadFile = (e.target as HTMLInputElement).files?.[0] ?? null;
                                gSubmitError = "";
                                core.scheduleRedraw();
                            }}
                        />
                        <span style={{ fontSize: "14px", color: gUploadFile ? "#111827" : "#6b7280" }}>
                            {gUploadFile ? gUploadFile.name : "Choose audio file (mp3, wav, flac, ogg, m4a…)"}
                        </span>
                    </label>
                )}
                <button
                    onClick={() => { if (canSubmit) submitJob(); }}
                    disabled={!canSubmit}
                    style={{ padding: "8px 16px", fontSize: "14px", cursor: canSubmit ? "pointer" : "default",
                             background: "#2563eb", color: "white", border: "none", borderRadius: "4px",
                             opacity: canSubmit ? 1 : 0.5, whiteSpace: "nowrap" }}
                >
                    {gSubmitting ? (gInputMode === "upload" ? "Uploading…" : "Submitting…") : "Make Karaoke"}
                </button>
            </div>
            <div style={{ display: "flex", alignItems: "center", gap: "16px", flexWrap: "wrap" }}>
                <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                    <label style={{ fontSize: "13px", color: "#374151", whiteSpace: "nowrap" }}>
                        Pitch:
                    </label>
                    <select
                        value={gPitchShift}
                        disabled={!!busy}
                        style={{ fontSize: "13px", padding: "4px 8px", border: "1px solid #d1d5db",
                                 borderRadius: "4px", background: "white", cursor: busy ? "default" : "pointer" }}
                        onChange={(e) => {
                            gPitchShift = parseInt((e.target as HTMLSelectElement).value, 10);
                            core.scheduleRedraw();
                        }}
                    >
                        {SEMITONE_OPTIONS.map(n => (
                            <option key={n} value={n}>{semitonelabel(n)}</option>
                        ))}
                    </select>
                </div>
                <div style={{ display: "flex", alignItems: "center", gap: "8px" }}>
                    <label style={{ fontSize: "13px", color: "#374151", whiteSpace: "nowrap" }}>
                        Speed:
                    </label>
                    <select
                        value={gSpeedAdjust}
                        disabled={!!busy}
                        style={{ fontSize: "13px", padding: "4px 8px", border: "1px solid #d1d5db",
                                 borderRadius: "4px", background: "white", cursor: busy ? "default" : "pointer" }}
                        onChange={(e) => {
                            gSpeedAdjust = parseFloat((e.target as HTMLSelectElement).value);
                            core.scheduleRedraw();
                        }}
                    >
                        {SPEED_OPTIONS.map(s => (
                            <option key={s} value={s}>{speedLabel(s)}</option>
                        ))}
                    </select>
                </div>
                {gSubmitError && <span style={{ color: "red", fontSize: "13px" }}>{gSubmitError}</span>}
            </div>
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
                : job.Step === "shifting"
                    ? "Adjusting audio…"
                    : job.Step === "transcribing"
                        ? "Transcribing lyrics…"
                        : job.Step === "analyzing"
                            ? "Detecting BPM and key…"
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
                <div style={{ display: "flex", alignItems: "center", gap: "6px", flexShrink: 0 }}>
                    <StatusBadge status={job.Status} />
                    <button
                        onClick={() => deleteJob(job.ID)}
                        title="Delete"
                        disabled={gDeletingIds.has(job.ID)}
                        style={{ background: "none", border: "none",
                                  cursor: gDeletingIds.has(job.ID) ? "default" : "pointer",
                                  padding: "2px 4px", color: "#9ca3af", fontSize: "16px",
                                  lineHeight: 1, borderRadius: "3px",
                                  opacity: gDeletingIds.has(job.ID) ? 0.4 : 1 }}
                        onMouseEnter={(e) => { if (!gDeletingIds.has(job.ID)) (e.target as HTMLElement).style.color = "#dc2626"; }}
                        onMouseLeave={(e) => { (e.target as HTMLElement).style.color = "#9ca3af"; }}
                    >✕</button>
                </div>
            </div>

            {job.Status === "error" && (() => {
                const isLong = job.Error.includes("\n") || job.Error.length > 200;
                const isExpanded = gExpandedErrors.has(job.ID);
                const displayed = isLong && !isExpanded
                    ? job.Error.split("\n")[0].slice(0, 200)
                    : job.Error;
                return (
                    <div style={{ marginTop: "8px", padding: "8px", background: "#fef2f2",
                                   borderRadius: "4px", fontSize: "13px", color: "#dc2626" }}>
                        <pre style={{ margin: 0, whiteSpace: "pre-wrap", wordBreak: "break-word",
                                       fontFamily: "inherit" }}>
                            {displayed}
                        </pre>
                        {isLong && (
                            <button
                                onClick={(e) => {
                                    e.stopPropagation();
                                    isExpanded ? gExpandedErrors.delete(job.ID) : gExpandedErrors.add(job.ID);
                                    core.scheduleRedraw();
                                }}
                                style={{ marginTop: "4px", background: "none", border: "none",
                                          padding: 0, cursor: "pointer", color: "#991b1b",
                                          fontSize: "12px", textDecoration: "underline" }}
                            >
                                {isExpanded ? "Show less" : "Show more"}
                            </button>
                        )}
                    </div>
                );
            })()}

            {job.Status === "done" && (
                <div style={{ marginTop: "10px" }}>
                    {(job.BPM > 0 || job.Key || job.VocalRange || job.PitchShift !== 0 || (job.SpeedAdjust !== 0 && job.SpeedAdjust !== 1.0)) && (
                        <div style={{ display: "flex", gap: "12px", marginBottom: "8px", fontSize: "13px", color: "#374151", flexWrap: "wrap" }}>
                            {job.BPM > 0 && <span><strong>BPM:</strong> {job.BPM}</span>}
                            {job.Key && <span><strong>Key:</strong> {job.Key}</span>}
                            {job.VocalRange && <span><strong>Vocal range:</strong> {job.VocalRange}</span>}
                            {job.PitchShift !== 0 && <span><strong>Pitch:</strong> {job.PitchShift > 0 ? `+${job.PitchShift}` : job.PitchShift} st</span>}
                            {job.SpeedAdjust !== 0 && job.SpeedAdjust !== 1.0 && <span><strong>Speed:</strong> {job.SpeedAdjust}×</span>}
                        </div>
                    )}
                    <audio controls src={`/jobs/${job.ID}/no_vocals.mp3`}
                           style={{ width: "100%", marginBottom: "8px" }} />
                    <div style={{ display: "flex", gap: "8px" }}>
                        <a href="#" onClick={() => { window.location.href = `/jobs/${job.ID}/no_vocals.mp3`; }}
                           style={downloadLinkStyle}>
                            ↓ Karaoke (no vocals)
                        </a>
                        <a href="#" onClick={() => { window.location.href = `/jobs/${job.ID}/vocals.mp3`; }}
                           style={downloadLinkStyle}>
                            ↓ Vocals only
                        </a>
                    </div>
                    {job.Lyrics && <LyricsBlock lyrics={job.Lyrics} jobId={job.ID} />}
                </div>
            )}
        </div>
    );
}

function LyricsBlock({ lyrics, jobId }: { lyrics: string; jobId: string }) {
    const expanded = gExpandedLyrics.has(jobId);
    const lines = lyrics.split(/\n+/).filter(l => l.trim());
    const preview = lines.slice(0, 4).join("\n");
    const isLong = lines.length > 4;
    return (
        <div style={{ marginTop: "10px" }}>
            <div style={{ fontSize: "12px", fontWeight: 600, color: "#374151", marginBottom: "4px", textTransform: "uppercase", letterSpacing: "0.05em" }}>Lyrics</div>
            <pre style={{ margin: 0, padding: "10px 12px", background: "#f9fafb", border: "1px solid #e5e7eb",
                          borderRadius: "4px", fontSize: "13px", color: "#374151",
                          whiteSpace: "pre-wrap", wordBreak: "break-word", fontFamily: "inherit",
                          lineHeight: "1.6" }}>
                {expanded ? lyrics : preview}
            </pre>
            {isLong && (
                <button
                    onClick={(e) => {
                        e.stopPropagation();
                        expanded ? gExpandedLyrics.delete(jobId) : gExpandedLyrics.add(jobId);
                        core.scheduleRedraw();
                    }}
                    style={{ marginTop: "4px", background: "none", border: "none", padding: 0,
                             cursor: "pointer", color: "#2563eb", fontSize: "12px", textDecoration: "underline" }}
                >
                    {expanded ? "Show less" : `Show all (${lines.length} lines)`}
                </button>
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

async function deleteJob(jobID: string) {
    if (gDeletingIds.has(jobID)) return;
    gDeletingIds.add(jobID);
    core.scheduleRedraw();

    const [, err] = await server.DeleteJob({ JobID: jobID });
    gDeletingIds.delete(jobID);

    if (err) {
        alert("Delete failed: " + err);
        core.scheduleRedraw();
        return;
    }
    gJobs = gJobs.filter(j => j.ID !== jobID);
    gExpandedErrors.delete(jobID);
    core.scheduleRedraw();
}

async function submitJob() {
    if (gSubmitting) return;
    if (gInputMode === "upload") {
        await uploadJob();
    } else {
        await submitUrlJob();
    }
}

async function submitUrlJob() {
    const url = gUrlInput.trim();
    if (!url) return;

    gSubmitting = true;
    gSubmitError = "";
    core.scheduleRedraw();

    const speed = gSpeedAdjust;
    const pitch = gPitchShift;
    const [resp, err] = await server.SubmitJob({ URL: url, PitchShift: pitch, SpeedAdjust: speed });
    gSubmitting = false;

    if (err || !resp) {
        gSubmitError = err || "unknown error";
        core.scheduleRedraw();
        return;
    }

    gUrlInput = "";
    gPitchShift = 0;
    gSpeedAdjust = 1.0;
    gActiveJobId = resp.JobID;

    // Optimistically add the job to the list (skip if server returned an existing job via dedup)
    if (!gJobs.find(j => j.ID === resp.JobID)) {
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
            BPM: 0,
            Key: "",
            PitchShift: pitch,
            Lyrics: "",
            SpeedAdjust: speed,
            VocalRange: "",
        };
        gJobs.unshift(newJob);
    } else {
        // Dedup returned an existing job; open history so user sees it
        gHistoryOpen = true;
    }

    startSSE(resp.JobID);
    core.scheduleRedraw();
}

async function uploadJob() {
    if (!gUploadFile) return;

    gSubmitting = true;
    gSubmitError = "";
    core.scheduleRedraw();

    const file = gUploadFile;
    const pitch = gPitchShift;
    const speed = gSpeedAdjust;

    const formData = new FormData();
    formData.append("audio", file);
    formData.append("pitchShift", String(pitch));
    formData.append("speedAdjust", String(speed));

    let jobID = "";
    try {
        const res = await nativeFetch("/upload", { method: "POST", body: formData });
        if (!res.ok) {
            const msg = await res.text();
            gSubmitError = msg || `upload failed (${res.status})`;
            gSubmitting = false;
            core.scheduleRedraw();
            return;
        }
        const data = await res.json();
        jobID = data.JobID;
    } catch (e: unknown) {
        gSubmitError = e instanceof Error ? e.message : "upload failed";
        gSubmitting = false;
        core.scheduleRedraw();
        return;
    }

    gSubmitting = false;
    gUploadFile = null;
    gPitchShift = 0;
    gSpeedAdjust = 1.0;
    gActiveJobId = jobID;

    const safeName = file.name.replace(/\.[^.]+$/, "");
    const newJob: Job = {
        ID: jobID,
        URL: "",
        Status: "queued",
        Step: "",
        Progress: 0,
        Title: safeName,
        CreatedAt: new Date().toISOString(),
        CompletedAt: new Date().toISOString(),
        Error: "",
        StepStartedAt: "",
        BPM: 0,
        Key: "",
        PitchShift: pitch,
        Lyrics: "",
        SpeedAdjust: speed,
        VocalRange: "",
    };
    gJobs.unshift(newJob);

    startSSE(newJob.ID);
    core.scheduleRedraw();
}

function startSSE(jobID: string) {
    stopSSE();
    const es = new EventSource(`/jobs/${jobID}/progress`);
    gEventSource = es;

    es.onmessage = (e) => {
        const job = JSON.parse(e.data) as Job;
        const idx = gJobs.findIndex(j => j.ID === job.ID);
        if (idx >= 0) gJobs[idx] = job;
        else gJobs.unshift(job);

        if (job.Status === "done" || job.Status === "error") stopSSE();
        core.scheduleRedraw();
    };

    es.onerror = () => stopSSE();
}

function stopSSE() {
    if (gEventSource) {
        gEventSource.close();
        gEventSource = null;
    }
}
