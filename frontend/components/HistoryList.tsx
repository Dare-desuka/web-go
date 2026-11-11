"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { thumbUrl, type Movie } from "@/lib/api";
import { listAll, me } from "@/lib/admin";
import { clearHistory, historyRows, type HistoryRow } from "@/lib/progress";

// Catalog comes from the server (same path Home uses) — fetching it from the
// browser meant /history silently showed nothing whenever the phone couldn't
// reach the API port. ponytail: fixed by removing the failing hop, not by
// error-handling it. See app/history/page.tsx.
//
// Server catalog is READY-only, so it dropped every film still in DRAFT and
// /history showed fewer rows than the Home "Riwayat" row. Admin catalogs must
// match, or the two pages disagree about the same localStorage.
export default function HistoryList({ movies }: { movies: Movie[] }) {
  const router = useRouter();
  const [catalog, setCatalog] = useState(movies);
  const [rows, setRows] = useState<HistoryRow[] | null>(null);

  useEffect(() => {
    me()
      .then((u) => {
        if (u && u.role === "admin") listAll().then(setCatalog).catch(() => {});
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    setRows(historyRows(catalog, 500));
  }, [catalog]);

  if (rows === null) return <p className="mt-10 text-subtext0">Memuat…</p>;

  return (
    <div className="mt-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">History</h1>
        {rows.length > 0 && (
          <button
            onClick={() => {
              if (!confirm("Hapus seluruh riwayat tontonan?")) return;
              clearHistory();
              setRows(historyRows(catalog, 500));
            }}
            className="rounded-sm border border-white/10 px-3 py-1.5 text-sm text-softred hover:bg-softred/10"
          >
            Hapus riwayat
          </button>
        )}
      </div>
      {rows.length === 0 ? (
        <p className="mt-4 text-subtext0">Belum ada riwayat tontonan.</p>
      ) : (
        <ul className="mt-4 flex flex-col gap-2">
          {rows.map((r) => (
            <li
              key={r.id}
              className="flex items-center gap-3 rounded-sm border border-white/10 bg-mantle p-3"
            >
              <button onClick={() => router.push(`/movies/${r.id}`)} className="shrink-0">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={r.art ?? thumbUrl(r.id)}
                  alt=""
                  loading="lazy"
                  className="aspect-video w-36 rounded-sm object-cover"
                />
              </button>
              <div className="min-w-0 flex-1">
                <Link href={`/movies/${r.id}`} className="truncate font-medium hover:text-mauve">
                  {r.title}
                </Link>
                <p className="font-mono text-xs text-overlay0">{r.pos}</p>
              </div>
              <Link
                href={`/movies/${r.id}`}
                className="shrink-0 rounded-sm bg-mauve px-3 py-1.5 text-sm font-bold text-crust hover:brightness-110"
              >
                Putar
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}