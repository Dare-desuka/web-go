"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import type { Movie } from "@/lib/api";
import { formatBytes } from "@/lib/api";
import {
  deleteSubtitle,
  getMovie,
  listSubtitlesAdmin,
  patchMovie,
  publishMovie,
  retryUpload,
  unpublishMovie,
  uploadPoster,
  uploadStatus,
  uploadSubtitle,
  uploadVideo,
  type AdminSubtitle,
  type UploadStatus,
} from "@/lib/admin";
import AdminGuard from "@/components/AdminGuard";

interface LogLine {
  at: string;
  kind: "info" | "ok" | "err";
  text: string;
}

const stamp = () =>
  new Date().toLocaleTimeString("id-ID", { hour12: false });

function MovieAdmin({ id }: { id: string }) {
  const [movie, setMovie] = useState<Movie | null>(null);
  const [status, setStatus] = useState<UploadStatus | null>(null);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [genres, setGenres] = useState("");
  const [error, setError] = useState("");
  const allowedGenres = ["comedy", "drama", "romance", "fantasy", "crime", "horror"];
  const [uploading, setUploading] = useState(false);
  const [hasFile, setHasFile] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);
  const posterRef = useRef<HTMLInputElement>(null);
  const backdropRef = useRef<HTMLInputElement>(null);
  const [fileName, setFileName] = useState("");
  const [posterName, setPosterName] = useState("");
  const [backdropName, setBackdropName] = useState("");
  const [subs, setSubs] = useState<AdminSubtitle[]>([]);
  const [subRows, setSubRows] = useState<
    { file: File; lang: string; label: string; note: string; done: boolean; err: string }[]
  >([]);
  const [subsRunning, setSubsRunning] = useState(false);
  const [sendPct, setSendPct] = useState<number | null>(null);
  const [lines, setLines] = useState<LogLine[]>([]);
  const lastParts = useRef(new Map<number, string>());
  const lastJobErr = useRef("");
  const lastMovieStatus = useRef("");
  const lastTick = useRef<{ t: number; b: number } | null>(null);
  const prevTick = useRef<{ t: number; b: number } | null>(null);
  const startedAt = useRef<number | null>(null);
  const logBox = useRef<HTMLDivElement>(null);

  const push = useCallback((kind: LogLine["kind"], text: string) => {
    setLines((ls) => [...ls.slice(-199), { at: stamp(), kind, text }]);
  }, []);

  // Record part transitions + job errors as log lines; tracks speed baseline.
  const noteStatus = useCallback(
    (s: UploadStatus) => {
      const key = (p: { status: string; retry_count: number }) => `${p.status}#${p.retry_count}`;
      if (lastParts.current.size === 0 && s.parts.length > 0) {
        for (const p of s.parts) lastParts.current.set(p.part_number, key(p));
        lastJobErr.current = s.job.error;
        lastMovieStatus.current = s.movie.status;
        push("info", `Status dimuat: ${s.movie.status}, ${s.parts.length} part.`);
        return;
      }
      for (const p of s.parts) {
        const prev = lastParts.current.get(p.part_number);
        if (prev === undefined) {
          lastParts.current.set(p.part_number, key(p));
          continue;
        }
        if (prev === key(p)) continue;
        lastParts.current.set(p.part_number, key(p));
        if (p.status === "COMPLETE") push("ok", `✓ Part ${p.part_number} selesai (${formatBytes(p.size)}).`);
        else if (p.status === "FAILED") push("err", `✗ Part ${p.part_number} gagal${p.last_error ? `: ${p.last_error}` : ""}.`);
        else if (p.status === "UPLOADING" && p.retry_count > 0)
          push("info", `Part ${p.part_number} retry ke-${p.retry_count}…`);
        else if (p.status === "UPLOADING") push("info", `Part ${p.part_number} mulai upload…`);
        else push("info", `Part ${p.part_number}: ${p.status}.`);
      }
      if (s.job.error && s.job.error !== lastJobErr.current) {
        lastJobErr.current = s.job.error;
        push("err", `Job: ${s.job.error}`);
      }
      if (s.movie.status !== lastMovieStatus.current) {
        lastMovieStatus.current = s.movie.status;
        if (s.movie.status === "READY") push("ok", "✓ Film READY — siap tayang.");
        else push("info", `Status film: ${s.movie.status}.`);
      }
      if (s.job.running) {
        if (lastTick.current) prevTick.current = lastTick.current;
        lastTick.current = { t: Date.now(), b: s.job.current_bytes };
      }
    },
    [push],
  );

  const applyStatus = useCallback(
    (s: UploadStatus) => {
      noteStatus(s);
      setStatus(s);
      setMovie(s.movie);
    },
    [noteStatus],
  );

  useEffect(() => {
    logBox.current?.scrollTo({ top: logBox.current.scrollHeight });
  }, [lines]);

  const load = useCallback(async () => {
    const [m, s, sub] = await Promise.all([
      getMovie(id),
      uploadStatus(id).catch(() => null),
      listSubtitlesAdmin(id).catch(() => [] as AdminSubtitle[]),
    ]);
    setMovie(m);
    setTitle(m.title);
    setDescription(m.description ?? "");
    setGenres(m.genres ?? "");
    setSubs(sub);
    if (s) applyStatus(s);
  }, [id, applyStatus]);

  useEffect(() => {
    load().catch(() => setError("Film tidak ditemukan."));
  }, [load]);

  // Poll while a pipeline runs.
  useEffect(() => {
    if (!status?.job.running) return;
    const t = setInterval(async () => {
      try {
        const s = await uploadStatus(id);
        applyStatus(s);
        if (!s.job.running) clearInterval(t);
      } catch {
        clearInterval(t);
      }
    }, 2000);
    return () => clearInterval(t);
  }, [id, status?.job.running, applyStatus]);

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      const m = await patchMovie(id, { title, description, genres });
      setMovie(m);
    } catch (err) {
      setError(String(err));
    }
  };

  const upload = async () => {
    const file = fileRef.current?.files?.[0];
    if (!movie) return;
    if (!file) {
      setError("Pilih file video dulu (tombol Pilih File).");
      return;
    }
    setError("");
    setUploading(true);
    setSendPct(0);
    startedAt.current = Date.now();
    lastTick.current = null;
    push("info", `Mengirim ${file.name} (${formatBytes(file.size)}) ke server…`);
    try {
      await uploadVideo(movie.id, file, (loaded, total) => {
        if (total > 0) setSendPct(Math.min(100, Math.round((loaded / total) * 100)));
      });
      push("ok", "File diterima server, pipeline dimulai.");
      const s = await uploadStatus(id);
      applyStatus(s);
    } catch (err) {
      push("err", `Upload gagal: ${String(err)}`);
      setError(String(err));
    } finally {
      setUploading(false);
      setSendPct(null);
      setHasFile(false);
      setFileName("");
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  const sendPoster = async (kind: "poster" | "backdrop", input: HTMLInputElement | null) => {
    const file = input?.files?.[0];
    if (!movie) return;
    if (!file) {
      setError("Pilih file gambar dulu.");
      return;
    }
    setError("");
    try {
      const m = await uploadPoster(movie.id, kind, file);
      setMovie(m);
      if (input) input.value = "";
      if (kind === "poster") setPosterName("");
      else setBackdropName("");
    } catch (err) {
      setError(String(err));
    }
  };

  const LANG_LABELS: Record<string, string> = {
    id: "Indonesia",
    en: "English",
    ja: "Japanese",
    zh: "Chinese",
    ms: "Melayu",
    ar: "Arabic",
    ko: "Korean",
  };

  // "SONE-970_ID.srt" → id; "Film.en.vtt", "Film-ja.srt", "Film (zh).vtt", "indonesia.srt" also work.
  // ponytail: code must be known — "final cut.srt" must NOT guess lang "cut"
  const KNOWN_LANGS = new Set([...Object.keys(LANG_LABELS), "th", "vi", "tl", "es", "fr", "de", "pt", "ru", "hi", "tr", "nl", "it", "jv", "su"]);
  const guessSubLang = (filename: string): { lang: string; label: string } => {
    const name = filename.replace(/\.[^.]+$/, "");
    const full = name.trim().toLowerCase();
    const byName = Object.entries(LANG_LABELS).find(([, v]) => v.toLowerCase() === full);
    const m =
      name.match(/[._\-\s\[\(]([a-z]{2,8})[\]\)]?$/i) ??
      name.match(/^([a-z]{2,8})[._\-\s]/i);
    const code = (byName?.[0] ?? m?.[1] ?? "").toLowerCase();
    if (!KNOWN_LANGS.has(code)) return { lang: "", label: "" };
    return { lang: code, label: LANG_LABELS[code] ?? code[0].toUpperCase() + code.slice(1) };
  };

  const pickSubs = (files: FileList | null) => {
    if (!files) return;
    setSubRows(
      [...files].map((file) => {
        const g = guessSubLang(file.name);
        return { file, lang: g.lang, label: g.label, note: "", done: false, err: "" };
      }),
    );
  };

  // ponytail: sequential loop over the existing single endpoint — no new backend route
  const uploadSubs = async () => {
    if (!movie || subRows.length === 0 || subsRunning) return;
    setError("");
    setSubsRunning(true);
    for (let i = 0; i < subRows.length; i++) {
      const row = subRows[i];
      if (row.done) continue;
      const lang = row.lang.trim().toLowerCase();
      const label = row.label.trim();
      if (!/^[a-z]{2,8}$/.test(lang) || !label) {
        setSubRows((rows) => rows.map((r, j) => (j === i ? { ...r, err: "isi bahasa (a-z) + label dulu" } : r)));
        continue;
      }
      setSubRows((rows) => rows.map((r, j) => (j === i ? { ...r, note: "mengirim…", err: "" } : r)));
      try {
        // ponytail: server may renumber colliding labels (Indonesia → Indonesia2)
        const saved = await uploadSubtitle(movie.id, row.file, lang, label);
        setSubRows((rows) =>
          rows.map((r, j) => (j === i ? { ...r, lang: saved.language, label: saved.label, note: "", done: true, err: "" } : r)),
        );
      } catch (err) {
        setSubRows((rows) => rows.map((r, j) => (j === i ? { ...r, note: "", err: String(err) } : r)));
      }
    }
    setSubs(await listSubtitlesAdmin(id).catch(() => [] as AdminSubtitle[]));
    setSubsRunning(false);
  };

  const flip = async (fn: (id: number) => Promise<Movie>) => {
    if (!movie) return;
    setError("");
    try {
      const m = await fn(movie.id);
      setMovie(m);
    } catch (err) {
      setError(String(err));
    }
  };

  if (!movie) return <p className="mt-10 text-subtext0">{error || "Memuat…"}</p>;

  const st = status;
  const terminal = movie.status === "READY" || movie.status === "ERROR";

  return (
    <div className="mt-6">
      <Link href="/admin" className="text-sm text-subtext0 hover:text-text">
        ← Dasbor
      </Link>
      <h1 className="mt-2 text-2xl font-bold">{movie.title}</h1>
      <p className="mt-1 font-mono text-xs text-overlay0">
        id {movie.id} · {movie.status} · {formatBytes(movie.video_size)} · {movie.video_mime || "-"}
      </p>
      <div className="mt-2 flex gap-2">
        {movie.status === "READY" && (
          <button
            onClick={() => flip(unpublishMovie)}
            className="rounded-sm border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
          >
            Unpublish (sembunyikan)
          </button>
        )}
        {movie.status === "DRAFT" && (
          <button
            onClick={() => flip(publishMovie)}
            className="rounded-sm border border-sapphire/40 px-3 py-1.5 text-sm text-sapphire hover:bg-sapphire/10"
          >
            Publish (tayangkan)
          </button>
        )}
      </div>

      <form onSubmit={save} className="mt-4 flex max-w-xl flex-col gap-3 rounded-sm border border-white/10 bg-mantle p-4">
        <h2 className="font-semibold">Metadata</h2>
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          onFocus={(e) => e.target.select()}
          className="rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none focus:border-mauve"
        />
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          placeholder="Deskripsi…"
          className="rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve"
        />
        <div className="flex flex-wrap gap-2">
          {allowedGenres.map((g) => {
            const active = genres.split(",").map((s) => s.trim()).includes(g);
            return (
              <button
                key={g}
                type="button"
                onClick={() => {
                  const set = new Set(genres.split(",").map((s) => s.trim()).filter(Boolean));
                  if (set.has(g)) set.delete(g);
                  else set.add(g);
                  setGenres([...set].join(","));
                }}
                className={`rounded-sm px-3 py-1 text-xs font-medium capitalize ${active ? "bg-mauve text-crust" : "border border-surface0 text-subtext0 hover:border-mauve hover:text-mauve"}`}
              >
                {g}
              </button>
            );
          })}
        </div>
        <button type="submit" className="w-fit rounded-sm bg-mauve px-4 py-2 text-sm font-bold text-crust hover:brightness-110">
          Simpan
        </button>
      </form>

      <div className="mt-4 max-w-xl rounded-sm border border-white/10 bg-mantle p-4">
        <h2 className="font-semibold">Video</h2>
        {movie.status !== "READY" && (
          <div className="mt-3 flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <label className="cursor-pointer rounded-sm border border-white/10 px-4 py-2 text-sm hover:bg-white/10">
                Pilih Video
                <input
                  ref={fileRef}
                  type="file"
                  accept="video/*,.mkv"
                  hidden
                  onChange={() => {
                    const f = fileRef.current?.files?.[0];
                    setHasFile(!!f);
                    setFileName(f?.name ?? "");
                  }}
                />
              </label>
              <span className="min-w-0 flex-1 truncate text-sm text-subtext0">
                {fileName || "Belum ada file dipilih"}
              </span>
            </div>
            <button
              onClick={upload}
              disabled={uploading || st?.job.running || !hasFile}
              className="rounded-sm bg-mauve px-4 py-2 text-sm font-bold text-crust hover:brightness-110 disabled:opacity-50"
            >
              {uploading ? "Mengirim…" : "Upload"}
            </button>
            {uploading && sendPct !== null && (
              <div>
                <div className="flex justify-between text-sm">
                  <span>Mengirim ke server</span>
                  <span className="font-mono">{sendPct}%</span>
                </div>
                <div className="h-2 w-full overflow-hidden rounded bg-black/50">
                  <div className="h-full bg-sapphire transition-all" style={{ width: `${sendPct}%` }} />
                </div>
              </div>
            )}
          </div>
        )}
        {error && <p className="mt-2 text-sm text-softred">{error}</p>}

        {st && (
          <div className="mt-4">
            <div className="flex justify-between text-sm">
              <span>Keseluruhan</span>
              <span className="font-mono">{st.overall.percent}%</span>
            </div>
            <div className="h-2 w-full overflow-hidden rounded bg-black/50">
              <div className="h-full bg-mauve transition-all" style={{ width: `${Math.min(st.overall.percent, 100)}%` }} />
            </div>
            <div className="mt-3 flex flex-col gap-2">
              {st.parts.map((p) => {
                const pct = p.size > 0 ? Math.round(((p.status === "COMPLETE" ? p.size : 0) / p.size) * 100) : 0;
                const live =
                  st.job.running && st.job.current_part === p.part_number && st.job.current_total > 0
                    ? Math.round((st.job.current_bytes / st.job.current_total) * 100)
                    : null;
                return (
                  <div key={p.part_number} className="text-sm">
                    <div className="flex justify-between">
                      <span>
                        Part {p.part_number} · {formatBytes(p.size)} ·{" "}
                        <span className="font-mono text-xs">{p.status}</span>
                        {p.retry_count > 0 && <span className="text-xs text-overlay0"> (retry {p.retry_count})</span>}
                      </span>
                      <span className="font-mono">{live ?? pct}%</span>
                    </div>
                    <div className="h-2 w-full overflow-hidden rounded bg-black/50">
                      <div className="h-full bg-mauve transition-all" style={{ width: `${Math.min(live ?? pct, 100)}%` }} />
                    </div>
                    {p.last_error && <p className="mt-1 text-xs text-softred">{p.last_error}</p>}
                  </div>
                );
              })}
            </div>
            {st.job.error && <p className="mt-2 text-sm text-softred">{st.job.error}</p>}
            {terminal && movie.status === "ERROR" && !st.job.running && (
              <button
                onClick={() => {
                  push("info", "Retry bagian yang gagal…");
                  retryUpload(movie.id).then(() => uploadStatus(id).then(applyStatus)).catch(() => setError("Retry gagal."));
                }}
                className="mt-3 rounded-sm border border-white/10 px-4 py-2 text-sm hover:bg-white/10"
              >
                Retry bagian yang gagal
              </button>
            )}
            {movie.status === "READY" && (
              <p className="mt-3 text-sm text-sapphire">✓ Upload selesai — film sudah tayang.</p>
            )}
            {st.job.running && (
              <p className="mt-3 font-mono text-xs text-subtext0">
                {(() => {
                  const t = prevTick.current;
                  const dt = t ? (Date.now() - t.t) / 1000 : 0;
                  const sp = t && dt > 0 ? (st.job.current_bytes - t.b) / dt : 0;
                  const el = startedAt.current ? ` · ${Math.floor((Date.now() - startedAt.current) / 1000)} dtk` : "";
                  return `Part ${st.job.current_part}: ${formatBytes(st.job.current_bytes)} / ${formatBytes(st.job.current_total)} · ${sp > 0 ? `${formatBytes(sp)}/s` : "…"}${el}`;
                })()}
              </p>
            )}
            <h3 className="mb-1 mt-4 font-mono text-[0.65rem] font-bold uppercase tracking-widest text-overlay0">
              Log proses
            </h3>
            <div
              ref={logBox}
              className="max-h-48 overflow-y-auto rounded-sm bg-black/50 p-3 font-mono text-xs leading-relaxed"
            >
              {lines.length === 0 && <p className="text-overlay0">Menunggu aktivitas…</p>}
              {lines.map((l, i) => (
                <p
                  key={i}
                  className={l.kind === "err" ? "text-softred" : l.kind === "ok" ? "text-sapphire" : "text-subtext1"}
                >
                  <span className="text-overlay0">[{l.at}]</span> {l.text}
                </p>
              ))}
            </div>
          </div>
        )}
      </div>


      <div className="mt-4 grid max-w-xl gap-4 md:grid-cols-2">
        <div className="rounded-sm border border-white/10 bg-mantle p-4">
          <h2 className="font-semibold">Poster</h2>
          {movie.poster_url && (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={movie.poster_url} alt="Poster" className="mt-2 max-h-40 rounded" />
          )}
          <div className="mt-2 flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <label className="cursor-pointer rounded-sm border border-white/10 px-3 py-1.5 text-xs hover:bg-white/10">
                Pilih File
                <input
                  ref={posterRef}
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  hidden
                  onChange={() => setPosterName(posterRef.current?.files?.[0]?.name ?? "")}
                />
              </label>
              <span className="min-w-0 flex-1 truncate text-xs text-subtext0">
                {posterName || "Belum ada file"}
              </span>
            </div>
            <button
              onClick={() => sendPoster("poster", posterRef.current)}
              className="rounded-sm bg-mauve px-3 py-1.5 text-xs font-bold text-crust hover:brightness-110"
            >
              Upload
            </button>
          </div>
        </div>
        <div className="rounded-sm border border-white/10 bg-mantle p-4">
          <h2 className="font-semibold">Backdrop</h2>
          {movie.backdrop_url && (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={movie.backdrop_url} alt="Backdrop" className="mt-2 max-h-40 rounded" />
          )}
          <div className="mt-2 flex flex-col gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <label className="cursor-pointer rounded-sm border border-white/10 px-3 py-1.5 text-xs hover:bg-white/10">
                Pilih File
                <input
                  ref={backdropRef}
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  hidden
                  onChange={() => setBackdropName(backdropRef.current?.files?.[0]?.name ?? "")}
                />
              </label>
              <span className="min-w-0 flex-1 truncate text-xs text-subtext0">
                {backdropName || "Belum ada file"}
              </span>
            </div>
            <button
              onClick={() => sendPoster("backdrop", backdropRef.current)}
              className="rounded-sm bg-mauve px-3 py-1.5 text-xs font-bold text-crust hover:brightness-110"
            >
              Upload
            </button>
          </div>
        </div>
      </div>

      <div className="mt-4 max-w-xl rounded-sm border border-white/10 bg-mantle p-4">
        <h2 className="font-semibold">Subtitle (.vtt/.srt)</h2>
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <label className="cursor-pointer rounded-sm border border-white/10 px-3 py-1.5 text-xs hover:bg-white/10">
            Pilih File (banyak)
            <input type="file" accept=".vtt,.srt" multiple hidden onChange={(e) => pickSubs(e.target.files)} />
          </label>
          <span className="text-xs text-subtext0">
            {subRows.length > 0 ? `${subRows.length} file` : "Belum ada file"} · bahasa ditebak dari nama, bisa diubah
          </span>
          <button
            onClick={uploadSubs}
            disabled={subsRunning || subRows.length === 0}
            className="rounded-sm bg-mauve px-3 py-1.5 text-xs font-bold text-crust hover:brightness-110 disabled:opacity-50"
          >
            {subsRunning ? "Mengupload…" : "Upload Semua"}
          </button>
        </div>
        {subRows.length > 0 && (
          <div className="mt-2 flex flex-col gap-2">
            {subRows.map((r, i) => (
              <div key={i} className="rounded-sm border border-white/10 bg-base p-2">
                <p className="truncate text-xs font-medium" title={r.file.name}>
                  {r.file.name}
                </p>
                <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                  <input
                    value={r.lang}
                    onChange={(e) => setSubRows((rows) => rows.map((x, j) => (j === i ? { ...x, lang: e.target.value } : x)))}
                    placeholder="id"
                    className="w-16 rounded-sm border border-surface0 bg-mantle px-2 py-1 text-xs outline-none focus:border-mauve"
                  />
                  <input
                    value={r.label}
                    onChange={(e) => setSubRows((rows) => rows.map((x, j) => (j === i ? { ...x, label: e.target.value } : x)))}
                    placeholder="Indonesia"
                    className="w-28 flex-1 rounded-sm border border-surface0 bg-mantle px-2 py-1 text-xs outline-none focus:border-mauve"
                  />
                  {r.done ? (
                    <span className="font-mono text-xs text-sapphire">✓</span>
                  ) : r.err ? (
                    <span className="font-mono text-[0.65rem] text-softred">{r.err}</span>
                  ) : (
                    r.note && <span className="font-mono text-[0.65rem] text-subtext0">{r.note}</span>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
        <ul className="mt-3 flex flex-col gap-2">
          {subs.length === 0 && <li className="text-sm text-subtext0">Belum ada subtitle.</li>}
          {subs.map((s) => (
            <li key={s.id} className="flex items-center justify-between rounded-sm border border-white/10 px-3 py-1.5 text-sm">
              <span>
                {s.label} <span className="font-mono text-xs text-overlay0">({s.language})</span>
              </span>
              <button
                onClick={() =>
                  deleteSubtitle(s.id)
                    .then(() => listSubtitlesAdmin(id))
                    .then(setSubs)
                    .catch(() => setError("Hapus subtitle gagal."))
                }
                className="text-xs text-softred hover:underline"
              >
                Hapus
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

export default function AdminMoviePage() {
  const routeParams = useParams<{ id: string }>();
  const [id, setId] = useState<string | null>(null);
  useEffect(() => {
    const resolved = routeParams?.id;
    if (resolved && resolved !== "_") setId(resolved);
    else {
      const m = window.location.pathname.match(/\/admin\/movies\/([^/]+)/);
      if (m) setId(decodeURIComponent(m[1]));
    }
  }, [routeParams]);
  return <AdminGuard>{id ? <MovieAdmin id={id} /> : <p className="mt-10 text-subtext0">Memuat…</p>}</AdminGuard>;
}
