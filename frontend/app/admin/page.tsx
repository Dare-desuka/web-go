"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import type { Movie } from "@/lib/api";
import {
  createMovie,
  createUser,
  deleteMovie,
  deleteUser,
  listAll,
  listUsers,
  logout,
  patchMovie,
  publishMovie,
  retryUpload,
  setUserRole,
  unpublishMovie,
  uploadVideo,
  type AdminAccount,
} from "@/lib/admin";
import { groupBySeries } from "@/lib/series";
import AdminGuard from "@/components/AdminGuard";

const badge: Record<string, string> = {
  READY: "bg-sapphire text-crust",
  ERROR: "bg-softred text-crust",
  UPLOADING: "bg-mauve text-crust",
  PROCESSING: "bg-lavender text-crust",
  DRAFT: "bg-white/10 text-subtext1",
  DELETING: "bg-white/10 text-subtext1",
};

function Dashboard() {
  const router = useRouter();
  const [movies, setMovies] = useState<Movie[]>([]);
  const [users, setUsers] = useState<AdminAccount[]>([]);
  const [creating, setCreating] = useState(false);
  const [q, setQ] = useState("");
  const [letter, setLetter] = useState(""); // "" = semua, "A"-"Z", "#" = non-huruf
  const [page, setPage] = useState(1);
  const PAGE_SIZE = 24;
  // Bulk metadata edit: checkbox per card + series quick-pick, PATCH per film.
  const [selectMode, setSelectMode] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [bulkGenres, setBulkGenres] = useState("");
  const [bulkDesc, setBulkDesc] = useState("");
  const [bulkYear, setBulkYear] = useState("");
  const [bulkRunning, setBulkRunning] = useState(false);
  const [notice, setNotice] = useState("");

  const filteredMovies = useMemo(() => {
    const needle = q.trim().toLowerCase();
    return movies.filter((m) => {
      if (letter) {
        const c = (m.title.trim()[0] ?? "").toUpperCase();
        if ((c >= "A" && c <= "Z" ? c : "#") !== letter) return false;
      }
      return !needle || m.title.toLowerCase().includes(needle);
    });
  }, [movies, letter, q]);

  const toggleSelect = (id: number) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  // Empty bulk field = keep existing (PATCH overwrites description unconditionally).
  const applyBulk = async () => {
    if (selected.size === 0 || bulkRunning) return;
    setError("");
    setNotice("");
    setBulkRunning(true);
    const byId = new Map(movies.map((m) => [m.id, m]));
    const year = /^\d{4}$/.test(bulkYear.trim()) ? parseInt(bulkYear.trim(), 10) : undefined;
    let ok = 0;
    let fail = 0;
    for (const id of selected) {
      const cur = byId.get(id);
      if (!cur) {
        fail++;
        continue;
      }
      try {
        await patchMovie(String(id), {
          description: bulkDesc.trim() === "" ? cur.description : bulkDesc.trim(),
          genres: bulkGenres.trim() === "" ? (cur.genres ?? "") : bulkGenres.trim(),
          year: year ?? cur.year,
        });
        ok++;
      } catch {
        fail++;
      }
    }
    setBulkRunning(false);
    setSelected(new Set());
    refresh();
    setNotice(`Selesai: ${ok} diperbarui${fail ? `, ${fail} gagal` : ""}.`);
  };

  const bulkPublish = async (on: boolean) => {
    if (selected.size === 0 || bulkRunning) return;
    setError("");
    setNotice("");
    setBulkRunning(true);
    let ok = 0;
    let fail = 0;
    for (const id of selected) {
      try {
        await (on ? publishMovie(id) : unpublishMovie(id));
        ok++;
      } catch {
        fail++;
      }
    }
    setBulkRunning(false);
    setSelected(new Set());
    refresh();
    setNotice(`Selesai: ${ok} ${on ? "dipublish" : "di-unpublish"}${fail ? `, ${fail} gagal` : ""}.`);
  };
  const [newUser, setNewUser] = useState("");
  const [newPass, setNewPass] = useState("");
  const [newRole, setNewRole] = useState("user");
  const [error, setError] = useState("");
  // Batch series upload: one base title, episode numbers from filenames.
  const [showBatch, setShowBatch] = useState(false);
  const [base, setBase] = useState("");
  const [bDesc, setBDesc] = useState("");
  const [bGenres, setBGenres] = useState("");
  const [bYear, setBYear] = useState("");
  const [batchFiles, setBatchFiles] = useState<File[]>([]);
  const [batchRows, setBatchRows] = useState<
    { name: string; title: string; note: string; pct: number | null; done: boolean; err: string }[]
  >([]);
  const [batchRunning, setBatchRunning] = useState(false);

  // "Show S2E05.mkv" + base "Show" → "Show S2 - 05"; "Show - 01.mp4" → "Show - 01".
  const batchTitle = (b: string, filename: string): { title: string } | { err: string } => {
    const name = filename.replace(/\.[^.]+$/, "").trim();
    // ponytail: (?![0-9]) not \b — "S2E05" has no boundary between 2 and E
    const sm = name.match(/\bS0?(\d{1,2})(?![0-9])/i) ?? name.match(/\bseason\s*0?(\d{1,2})\b/i);
    const season = sm ? parseInt(sm[1], 10) : 1;
    const rest = name.replace(/\bS0?\d{1,2}(?![0-9])/i, "").replace(/\bseason\s*0?\d{1,2}\b/i, "");
    const em = rest.match(/(\d{1,2})\s*$/);
    if (!em) return { err: "tidak ada nomor episode di nama file" };
    const ep = parseInt(em[1], 10);
    if (ep < 1 || ep > 99) return { err: "nomor episode di luar 1-99" };
    const nn = String(ep).padStart(2, "0");
    return { title: season > 1 ? `${b} S${season} - ${nn}` : `${b} - ${nn}` };
  };

  const deriveRows = (b: string, files: File[]) =>
    files.map((f) => {
      const r = batchTitle(b.trim(), f.name);
      return "err" in r
        ? { name: f.name, title: "", note: r.err, pct: null as number | null, done: false, err: r.err }
        : { name: f.name, title: r.title, note: "", pct: null as number | null, done: false, err: "" };
    });

  const pickBatch = (files: FileList | null) => {
    if (!files) return;
    const arr = [...files];
    setBatchFiles(arr);
    setBatchRows(deriveRows(base, arr));
  };

  // ponytail: sequential, one file at a time — parallel multi-GB uploads error out
  const runBatch = async () => {
    const b = base.trim();
    if (!b) {
      setError("Isi judul series dulu.");
      return;
    }
    if (batchFiles.length === 0 || batchRunning) return;
    setError("");
    setBatchRunning(true);
    const year = /^\d{4}$/.test(bYear.trim()) ? parseInt(bYear.trim(), 10) : undefined;
    for (let i = 0; i < batchFiles.length; i++) {
      const r = batchTitle(b, batchFiles[i].name);
      if ("err" in r) continue; // already flagged in preview
      setBatchRows((rows) => rows.map((row, j) => (j === i ? { ...row, title: r.title, note: "mengirim…", pct: 0, err: "" } : row)));
      try {
        const m = await createMovie({ title: r.title, description: bDesc.trim() || undefined, genres: bGenres.trim() || undefined, year });
        await uploadVideo(m.id, batchFiles[i], (loaded, total) => {
          const pct = total > 0 ? Math.min(100, Math.round((loaded / total) * 100)) : null;
          setBatchRows((rows) => rows.map((row, j) => (j === i ? { ...row, pct } : row)));
        });
        setBatchRows((rows) => rows.map((row, j) => (j === i ? { ...row, note: "terkirim, pipeline jalan", pct: 100, done: true } : row)));
      } catch (err) {
        setBatchRows((rows) => rows.map((row, j) => (j === i ? { ...row, note: "", err: String(err) } : row)));
      }
    }
    setBatchRunning(false);
    refresh();
  };

  const refresh = useCallback(async () => {
    try {
      setMovies(await listAll());
    } catch {
      setError("Gagal memuat daftar film.");
    }
    try {
      setUsers(await listUsers());
    } catch {
      setUsers([]);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const create = async () => {
    setError("");
    setCreating(true);
    try {
      const m = await createMovie({});
      router.push(`/admin/movies/${m.id}`);
    } catch (err) {
      setError(String(err));
    } finally {
      setCreating(false);
    }
  };

  const addUser = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    try {
      await createUser({ username: newUser.trim(), password: newPass, role: newRole });
      setNewUser("");
      setNewPass("");
      refresh();
    } catch (err) {
      setError(String(err));
    }
  };

  const flipRole = async (u: AdminAccount) => {
    setError("");
    try {
      await setUserRole(u.id, u.role === "admin" ? "user" : "admin");
      refresh();
    } catch (err) {
      setError(String(err));
    }
  };

  const removeUser = async (u: AdminAccount) => {
    if (!confirm(`Hapus akun "${u.username}"?`)) return;
    setError("");
    try {
      await deleteUser(u.id);
      refresh();
    } catch (err) {
      setError(String(err));
    }
  };

  return (
    <div className="mt-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Dasbor Admin</h1>
        <button
          onClick={() => logout().then(() => router.replace("/login"))}
          className="rounded-sm border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
        >
          Keluar
        </button>
      </div>

      <h2 className="mt-4 text-xl font-bold">Pengguna</h2>
      <form onSubmit={addUser} className="mt-3 flex flex-wrap gap-2">
        <input
          value={newUser}
          onChange={(e) => setNewUser(e.target.value)}
          placeholder="Username baru…"
          className="w-40 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve"
        />
        <input
          type="password"
          value={newPass}
          onChange={(e) => setNewPass(e.target.value)}
          placeholder="Password (min 8)…"
          className="w-40 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve"
        />
        <select
          value={newRole}
          onChange={(e) => setNewRole(e.target.value)}
          className="rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none focus:border-mauve"
        >
          <option value="user">user</option>
          <option value="admin">admin</option>
        </select>
        <button type="submit" className="rounded-sm bg-mauve px-4 py-2 text-sm font-bold text-crust hover:brightness-110">
          + Buat
        </button>
      </form>
      <div className="mt-3 flex flex-col gap-2">
        {users.map((u) => (
          <div key={u.id} className="flex items-center justify-between gap-3 rounded-sm border border-white/10 bg-mantle p-3">
            <div className="min-w-0">
              <span className="font-medium">{u.username}</span>
              <span className={`ml-2 rounded px-1.5 py-0.5 font-mono text-[0.65rem] font-bold ${u.role === "admin" ? "bg-mauve text-crust" : "bg-white/10 text-subtext1"}`}>
                {u.role}
              </span>
            </div>
            <div className="flex shrink-0 gap-2">
              <button
                onClick={() => flipRole(u)}
                title={u.role === "admin" ? "Turunkan ke user" : "Naikkan ke admin"}
                className="rounded-sm border border-white/10 px-3 py-1.5 text-sm hover:bg-white/10"
              >
                {u.role === "admin" ? "↓ user" : "↑ admin"}
              </button>
              <button
                onClick={() => removeUser(u)}
                className="rounded-sm border border-softred/40 px-3 py-1.5 text-sm text-softred hover:bg-softred/10"
              >
                Hapus
              </button>
            </div>
          </div>
        ))}
      </div>

      <div className="mt-10 flex flex-wrap items-center gap-2">
        <button
          onClick={create}
          disabled={creating}
          className="rounded-sm bg-mauve px-4 py-2 text-sm font-bold text-crust hover:brightness-110 disabled:opacity-50"
        >
          {creating ? "Membuat…" : "+ Tambah Film"}
        </button>
        <input
          value={q}
          onChange={(e) => {
            setQ(e.target.value);
            setPage(1);
          }}
          placeholder="Cari film…"
          className="min-w-0 flex-1 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve sm:max-w-xs"
        />
        {q && (
          <button onClick={() => setQ("")} className="rounded-sm border border-white/10 px-2 py-2 text-xs hover:bg-white/10">
            ×
          </button>
        )}
        <button
          onClick={() => setShowBatch((v) => !v)}
          className="rounded-sm border border-mauve/40 px-4 py-2 text-sm font-bold text-mauve hover:bg-mauve/10"
        >
          {showBatch ? "Tutup Batch" : "+ Batch Series"}
        </button>
        <button
          onClick={() => {
            setSelectMode((v) => !v);
            setSelected(new Set());
          }}
          className="rounded-sm border border-sapphire/40 px-4 py-2 text-sm font-bold text-sapphire hover:bg-sapphire/10"
        >
          {selectMode ? "Batal Pilih" : "Pilih"}
        </button>
      </div>
      {selectMode && (
        <div className="mt-3 rounded-sm border border-sapphire/30 bg-mantle p-4">
          <p className="text-sm text-subtext0">
            {selected.size} film dipilih · kolom kosong = pertahankan nilai lama.
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <select
              defaultValue=""
              onChange={(e) => {
                const g = groupBySeries(movies).find((x) => x.key === e.target.value);
                if (g) setSelected((prev) => new Set([...prev, ...g.movies.map((m) => m.id)]));
                e.target.value = "";
              }}
              className="rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none focus:border-sapphire"
            >
              <option value="">+ Pilih 1 series…</option>
              {groupBySeries(movies)
                .filter((g) => !g.key.startsWith("__single:"))
                .map((g) => (
                  <option key={g.key} value={g.key}>
                    {g.label} ({g.movies.length} eps)
                  </option>
                ))}
            </select>
            <button
              onClick={() => setSelected(new Set(filteredMovies.map((m) => m.id)))}
              className="rounded-sm border border-white/10 px-3 py-2 text-sm hover:bg-white/10"
            >
              Pilih hasil filter
            </button>
            <button
              onClick={() => setSelected(new Set())}
              className="rounded-sm border border-white/10 px-3 py-2 text-sm hover:bg-white/10"
            >
              Bersihkan
            </button>
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {["comedy", "drama", "romance", "fantasy", "crime", "horror"].map((g) => {
              const set = new Set(bulkGenres.split(",").map((s) => s.trim()).filter(Boolean));
              const active = set.has(g);
              return (
                <button
                  key={g}
                  type="button"
                  onClick={() => {
                    if (active) set.delete(g);
                    else set.add(g);
                    setBulkGenres([...set].join(","));
                  }}
                  className={`rounded-sm px-3 py-1 text-xs font-medium capitalize ${active ? "bg-sapphire text-crust" : "border border-surface0 text-subtext0 hover:border-sapphire hover:text-sapphire"}`}
                >
                  {g}
                </button>
              );
            })}
          </div>
          <div className="mt-2 flex flex-wrap gap-2">
            <input
              value={bulkDesc}
              onChange={(e) => setBulkDesc(e.target.value)}
              placeholder="Deskripsi baru (kosong = tetap)…"
              className="min-w-0 flex-1 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-sapphire sm:max-w-xs"
            />
            <input
              value={bulkYear}
              onChange={(e) => setBulkYear(e.target.value)}
              placeholder="Tahun"
              className="w-28 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-sapphire"
            />
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            <button
              onClick={applyBulk}
              disabled={bulkRunning || selected.size === 0}
              className="rounded-sm bg-sapphire px-4 py-2 text-sm font-bold text-crust hover:brightness-110 disabled:opacity-50"
            >
              {bulkRunning ? "Menerapkan…" : `Terapkan ke ${selected.size} film`}
            </button>
            <button
              onClick={() => bulkPublish(true)}
              disabled={bulkRunning || selected.size === 0}
              className="rounded-sm border border-white/10 px-3 py-2 text-sm hover:bg-white/10 disabled:opacity-50"
            >
              Publish
            </button>
            <button
              onClick={() => bulkPublish(false)}
              disabled={bulkRunning || selected.size === 0}
              className="rounded-sm border border-white/10 px-3 py-2 text-sm hover:bg-white/10 disabled:opacity-50"
            >
              Unpublish
            </button>
          </div>
        </div>
      )}
      {notice && <p className="mt-2 text-sm text-sapphire">{notice}</p>}
      {showBatch && (
        <div className="mt-3 rounded-sm border border-mauve/30 bg-mantle p-4">
          <p className="text-sm text-subtext0">
            Isi judul series sekali — nomor episode diambil dari nama file (<span className="font-mono">- 01</span>,{" "}
            <span className="font-mono">Eps01</span>, <span className="font-mono">S2E05</span>). Upload jalan 1 per 1.
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            <input
              value={base}
              onChange={(e) => {
                setBase(e.target.value);
                if (batchFiles.length > 0) setBatchRows(deriveRows(e.target.value, batchFiles));
              }}
              placeholder="Judul series… (mis. A Bone Fide Killer)"
              className="min-w-0 flex-1 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve sm:max-w-xs"
            />
            <input
              value={bDesc}
              onChange={(e) => setBDesc(e.target.value)}
              placeholder="Deskripsi (opsional)…"
              className="min-w-0 flex-1 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve sm:max-w-xs"
            />
            <input
              value={bYear}
              onChange={(e) => setBYear(e.target.value)}
              placeholder="Tahun (opsional)"
              className="w-32 rounded-sm border border-surface0 bg-base px-3 py-2 text-sm outline-none placeholder:text-overlay0 focus:border-mauve"
            />
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {["comedy", "drama", "romance", "fantasy", "crime", "horror"].map((g) => {
              const set = new Set(bGenres.split(",").map((s) => s.trim()).filter(Boolean));
              const active = set.has(g);
              return (
                <button
                  key={g}
                  type="button"
                  onClick={() => {
                    if (active) set.delete(g);
                    else set.add(g);
                    setBGenres([...set].join(","));
                  }}
                  className={`rounded-sm px-3 py-1 text-xs font-medium capitalize ${active ? "bg-mauve text-crust" : "border border-surface0 text-subtext0 hover:border-mauve hover:text-mauve"}`}
                >
                  {g}
                </button>
              );
            })}
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <label className="cursor-pointer rounded-sm border border-white/10 px-4 py-2 text-sm hover:bg-white/10">
              Pilih File (banyak)
              <input type="file" accept="video/*,.mkv" multiple hidden onChange={(e) => pickBatch(e.target.files)} />
            </label>
            <span className="text-sm text-subtext0">{batchFiles.length > 0 ? `${batchFiles.length} file` : "Belum ada file"}</span>
            <button
              onClick={runBatch}
              disabled={batchRunning || batchFiles.length === 0}
              className="rounded-sm bg-mauve px-4 py-2 text-sm font-bold text-crust hover:brightness-110 disabled:opacity-50"
            >
              {batchRunning ? "Mengupload…" : "Upload Semua"}
            </button>
          </div>
          {batchRows.length > 0 && (
            <div className="mt-3 flex flex-col gap-2">
              {batchRows.map((r, i) => (
                <div key={i} className="rounded-sm border border-white/10 bg-base p-2.5 text-sm">
                  <div className="flex items-center justify-between gap-2">
                    <span className="min-w-0 truncate font-medium" title={r.title || r.name}>
                      {r.title || r.name}
                    </span>
                    {r.done ? (
                      <span className="shrink-0 font-mono text-xs text-sapphire">✓ terkirim</span>
                    ) : r.err ? (
                      <span className="shrink-0 font-mono text-xs text-softred">{r.err}</span>
                    ) : (
                      <span className="shrink-0 font-mono text-xs text-subtext0">{r.note || r.name}</span>
                    )}
                  </div>
                  {r.pct !== null && !r.done && (
                    <div className="mt-1.5 h-1.5 w-full overflow-hidden rounded bg-black/50">
                      <div className="h-full bg-mauve transition-all" style={{ width: `${r.pct}%` }} />
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      {error && <p className="mt-2 text-sm text-softred">{error}</p>}

      {(() => {
        // ponytail: client-side filter + letters + paging, no new endpoint; server search when list > 1k
        const present = new Set<string>();
        for (const m of movies) {
          const c = (m.title.trim()[0] ?? "").toUpperCase();
          present.add(c >= "A" && c <= "Z" ? c : "#");
        }
        const letters = ["", ...Array.from({ length: 26 }, (_, i) => String.fromCharCode(65 + i)), "#"];
        const filtered = filteredMovies;
        const pages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
        const cur = Math.min(page, pages);
        const visible = filtered.slice((cur - 1) * PAGE_SIZE, cur * PAGE_SIZE);
        return (
          <>
            <div className="mt-6 flex flex-wrap gap-1">
              {letters.map((L) => {
                const active = letter === L;
                const empty = L !== "" && !present.has(L);
                return (
                  <button
                    key={L || "all"}
                    disabled={empty}
                    onClick={() => {
                      setLetter(L);
                      setPage(1);
                    }}
                    title={L === "" ? "Semua" : L === "#" ? "Non-huruf" : `Judul berawalan ${L}`}
                    className={`rounded-sm px-2 py-1 font-mono text-xs font-bold ${
                      active
                        ? "bg-mauve text-crust"
                        : empty
                          ? "cursor-not-allowed text-overlay0/40"
                          : "border border-surface0 text-subtext0 hover:border-mauve hover:text-mauve"
                    }`}
                  >
                    {L === "" ? "∗" : L}
                  </button>
                );
              })}
            </div>
            <p className="mt-2 text-xs text-overlay0">
              {filtered.length} / {movies.length} film · hal {cur}/{pages}
            </p>
            <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
              {movies.length === 0 && <p className="text-subtext0">Belum ada film.</p>}
              {movies.length > 0 && filtered.length === 0 && (
                <p className="text-sm text-subtext0">Tidak ada hasil.</p>
              )}
              {visible.map((m) => (
            <div key={m.id} className="flex justify-between gap-2 rounded-sm border border-white/10 bg-mantle p-2.5">
              <div className="min-w-0">
                <div className="flex items-center gap-1.5">
                  {selectMode && (
                    <input
                      type="checkbox"
                      checked={selected.has(m.id)}
                      onChange={() => toggleSelect(m.id)}
                      aria-label={`Pilih ${m.title}`}
                      className="h-4 w-4 shrink-0 accent-[#74c7ec]"
                    />
                  )}
                  <Link href={`/admin/movies/${m.id}`} className="block min-w-0 flex-1 truncate text-sm font-medium hover:underline" title={m.title}>
                    {m.title}
                  </Link>
                </div>
                <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                  <span className={`rounded px-1.5 py-0.5 font-mono text-[0.65rem] font-bold ${badge[m.status] ?? badge.DRAFT}`}>
                    {m.status}
                  </span>
                </div>
                <div className="mt-2 flex gap-1.5">
                  <Link
                    href={`/admin/movies/${m.id}`}
                    className="rounded-sm border border-white/10 px-2 py-1 text-xs hover:bg-white/10"
                  >
                    Edit
                  </Link>
                  {m.status === "ERROR" && (
                    <button
                      onClick={() => retryUpload(m.id).then(refresh).catch(() => setError("Retry gagal."))}
                      className="rounded-sm border border-white/10 px-2 py-1 text-xs hover:bg-white/10"
                    >
                      Retry
                    </button>
                  )}
                </div>
              </div>
              <div className="flex shrink-0 flex-col items-end justify-between">
                {(m.subtitle_count ?? 0) > 0 && (
                  <span className="rounded bg-mauve/20 px-1.5 py-0.5 font-mono text-[0.65rem] font-bold text-mauve" title="Subtitle terupload">
                    {m.subtitle_count} sub
                  </span>
                )}
                <button
                  onClick={() => {
                    if (!confirm(`Hapus "${m.title}"?`)) return;
                    deleteMovie(m.id).then(refresh).catch(() => setError("Hapus gagal."));
                  }}
                  className="mt-auto rounded-sm border border-softred/40 px-2 py-1 text-xs text-softred hover:bg-softred/10"
                >
                  Hapus
                </button>
              </div>
            </div>
              ))}
            </div>
            {pages > 1 && (
              <div className="mt-4 flex items-center justify-center gap-2">
                <button
                  onClick={() => setPage((p) => Math.max(1, p - 1))}
                  disabled={cur <= 1}
                  className="rounded-sm border border-surface0 px-3 py-1.5 text-sm hover:border-mauve hover:text-mauve disabled:opacity-40"
                >
                  ‹
                </button>
                <span className="font-mono text-xs text-subtext0">
                  {cur} / {pages}
                </span>
                <button
                  onClick={() => setPage((p) => Math.min(pages, p + 1))}
                  disabled={cur >= pages}
                  className="rounded-sm border border-surface0 px-3 py-1.5 text-sm hover:border-mauve hover:text-mauve disabled:opacity-40"
                >
                  ›
                </button>
              </div>
            )}
          </>
        );
      })()}
    </div>
  );
}

export default function AdminPage() {
  return (
    <AdminGuard>
      <Dashboard />
    </AdminGuard>
  );
}
