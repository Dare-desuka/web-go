"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { listMoviesClient, type Movie } from "@/lib/api";
import { groupBySeries } from "@/lib/series";
import SeriesCard from "@/components/SeriesCard";

// Pornhub's "All Pornstars & Models": flat directory of every series.
export default function SeriesIndex() {
  const [movies, setMovies] = useState<Movie[]>([]);
  useEffect(() => {
    listMoviesClient().then(setMovies);
  }, []);

  const real = useMemo(
    () =>
      groupBySeries(movies)
        .sort(
          (a, b) => Math.max(...b.movies.map((m) => m.updated_at)) - Math.max(...a.movies.map((m) => m.updated_at)),
        )
        .filter((s) => s.movies.length > 1),
    [movies],
  );

  return (
    <div className="mt-2">
      <h1 className="text-sm font-bold uppercase text-subtext1">
        <span className="text-mauve">▸</span> Semua Series
        <span className="ml-2 text-xs font-normal text-overlay0">{real.length}</span>
      </h1>
      {real.length === 0 ? (
        <p className="mt-6 text-sm text-subtext0">Belum ada series multi-episode.</p>
      ) : (
        <div className="mt-4 grid grid-cols-2 gap-x-2.5 gap-y-4 sm:grid-cols-3 md:grid-cols-4">
          {real.map((s) => (
            <SeriesCard key={s.key} group={s} />
          ))}
        </div>
      )}
      <p className="mt-8 text-xs text-overlay0">
        Diurut dari yang terbaru diperbarui.{" "}
        <Link href="/home" className="hover:text-mauve">
          Kembali ke beranda
        </Link>
        .
      </p>
    </div>
  );
}