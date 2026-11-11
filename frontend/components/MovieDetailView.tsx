"use client";

import { useEffect, useMemo, useState } from "react";
import Link from "next/link";
import { notFound, useParams, useRouter } from "next/navigation";
import type { Movie, Subtitle } from "@/lib/api";
import { downloadUrl, formatBytes, getMovieClient, listMoviesClient, listSubtitlesClient } from "@/lib/api";
import { getMovie as getMovieAdmin, listSubtitlesAdmin, me } from "@/lib/admin";
import { formatTime } from "@/lib/progress";
import Player from "@/components/Player";
import WatchedButton from "@/components/WatchedButton";
import BookmarkButton from "@/components/BookmarkButton";
import { publicBase, thumbUrl } from "@/lib/api";
import { seriesKeyForTitle } from "@/lib/series";

// ponytail: read the id from the URL via useParams(), not from the params
// prop. In a static export the params prop is frozen to the sentinel ("_"),
// so the runtime value would never reach the component. useParams() reads
// the actual path at runtime.
export default function MovieDetail() {
  const router = useRouter();
  const routeParams = useParams<{ id: string }>();
  const [id, setId] = useState<string | null>(null);
  const [movie, setMovie] = useState<Movie | null>(null);
  const [subtitles, setSubtitles] = useState<Subtitle[]>([]);
  const [missing, setMissing] = useState(false);
  const [related, setRelated] = useState<Movie[]>([]);
  const [isAdmin, setIsAdmin] = useState(false);
  const [thumbBusy, setThumbBusy] = useState(false);
  const [thumbMsg, setThumbMsg] = useState<string | null>(null);
  const [thumbKey, setThumbKey] = useState(0);

  useEffect(() => {
    const resolved = routeParams?.id;
    if (resolved && resolved !== "_") setId(resolved);
    else {
      const m = window.location.pathname.match(/\/movies\/([^/]+)/);
      if (m) setId(decodeURIComponent(m[1]));
    }
  }, [routeParams]);

  useEffect(() => {
    if (!id) return;
    me()
      .catch(() => null)
      .then(async (u) => {
        const admin = !!u && u.role === "admin";
        setIsAdmin(admin);
        const [m, s] = await Promise.all([
          admin ? getMovieAdmin(id).catch(() => null) : getMovieClient(id),
          admin ? listSubtitlesAdmin(id).catch(() => []) : listSubtitlesClient(id),
        ]);
        if (!m) {
          setMissing(true);
          return;
        }
        setMovie(m);
        setSubtitles(s);
      })
      .catch(() => setMissing(true));
  }, [id]);

  // Related: other films, prefer same genre first. Excludes the current movie's
  // own series (episodes live on the series page) and caps 2 per series so the
  // rail does not become 12 episodes of one show.
  useEffect(() => {
    if (!movie) return;
    listMoviesClient()
      .then((all) => {
        const genres = new Set((movie.genres ?? "").split(",").map((s) => s.trim()).filter(Boolean));
        const myKey = seriesKeyForTitle(movie.title, movie.id);
        const ranked = all
          .filter((m) => m.id !== movie.id && seriesKeyForTitle(m.title, m.id) !== myKey)
          .map((m) => ({
            m,
            score: (m.genres ?? "").split(",").filter((g) => genres.has(g.trim())).length,
          }))
          .sort((a, b) => b.score - a.score || a.m.title.localeCompare(b.m.title));

        const seen = new Map<string, number>();
        const capped: Movie[] = [];
        for (const { m } of ranked) {
          const k = seriesKeyForTitle(m.title, m.id);
          const n = seen.get(k) ?? 0;
          if (n >= 2) continue;
          seen.set(k, n + 1);
          capped.push(m);
          if (capped.length >= 24) break;
        }
        setRelated(capped);
      })
      .catch(() => {});
  }, [movie]);

  const sidebarRelated = useMemo(() => related.slice(0, 12), [related]);

  if (missing) notFound();
  if (!movie) return <p className="mt-10 text-subtext0">Memuat…</p>;

  const genres = (movie.genres ?? "").split(",").map((s) => s.trim()).filter(Boolean);

  async function handleGenerateThumb() {
    if (!id || thumbBusy) return;
    setThumbBusy(true);
    setThumbMsg(null);
    try {
      const res = await fetch(`${publicBase}/api/admin/movies/${id}/thumbnail`, {
        method: "POST",
        credentials: "include",
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body?.error ?? `HTTP ${res.status}`);
      }
      setThumbMsg("Thumbnail dibuat.");
      setThumbKey((k) => k + 1);
    } catch (e) {
      setThumbMsg(`Gagal: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setThumbBusy(false);
    }
  }

  return (
    <div className="mt-2">
      <button
        onClick={() => (window.history.length > 1 ? router.back() : router.push("/home"))}
        className="text-xs uppercase text-subtext0 hover:text-mauve"
      >
        ← Kembali
      </button>

      {/* Pornhub watch layout: player left, related list right. */}
      <div className="mt-3 flex flex-col gap-5 lg:flex-row">
        <div className="min-w-0 flex-1">
          <div className="overflow-hidden rounded-sm bg-black">
            <Player movie={movie} subtitles={subtitles} />
          </div>

          <h1 className="mt-4 text-2xl font-bold leading-tight">{movie.title}</h1>

          <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-subtext0">
            {movie.status !== "READY" && (
              <span className="rounded-sm bg-softred/20 px-1.5 py-0.5 font-bold text-softred">{movie.status}</span>
            )}
            <span>{formatBytes(movie.video_size)}</span>
            {movie.duration_seconds ? <span>{formatTime(movie.duration_seconds)}</span> : null}
            {movie.year ? <span>{movie.year}</span> : null}
            {movie.subtitle_count ? <span>{movie.subtitle_count} subtitle</span> : null}
          </div>

          {isAdmin && (
            <div className="mt-3 flex items-center gap-2">
              <button
                onClick={handleGenerateThumb}
                disabled={thumbBusy}
                className="rounded-sm bg-mauve px-3 py-1.5 text-xs font-bold text-crust disabled:opacity-50"
              >
                {thumbBusy ? "Generating…" : "Generate thumbnail"}
              </button>
              {thumbMsg && <span className="text-xs text-subtext0">{thumbMsg}</span>}
            </div>
          )}

          {genres.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-1.5">
              {genres.map((g) => (
                <Link
                  key={g}
                  href={`/home?genre=${encodeURIComponent(g)}`}
                  className="rounded-sm bg-white/10 px-2 py-0.5 text-xs font-medium capitalize text-subtext1 hover:bg-mauve hover:text-crust"
                >
                  {g}
                </Link>
              ))}
            </div>
          )}

          {movie.description && (
            <p className="mt-4 max-w-3xl rounded-sm border border-white/10 bg-crust p-3 text-sm leading-relaxed text-subtext1">
              {movie.description}
            </p>
          )}

          <div className="mt-4 flex flex-wrap gap-2">
            <WatchedButton movieID={movie.id} />
            <BookmarkButton movieID={movie.id} />
            <a href={downloadUrl(movie.id)} className="rounded-sm bg-mauve px-5 py-2 text-sm font-bold text-crust hover:brightness-110">
              ⬇ Download
            </a>
          </div>

          {subtitles.length > 0 && (
            <div className="mt-6">
              <h2 className="text-xs font-bold uppercase text-subtext1">Subtitle</h2>
              <ul className="mt-2 flex flex-wrap gap-1.5">
                {subtitles.map((s) => (
                  <li key={s.id} className="rounded-sm border border-white/10 px-2.5 py-1 text-xs text-subtext1">
                    {s.label}
                  </li>
                ))}
              </ul>
              <p className="mt-2 text-xs text-overlay0">Pilih bahasa dari menu CC di player.</p>
            </div>
          )}
        </div>

        {/* Related list, Pornhub right rail. */}
        <aside className="w-full shrink-0 lg:w-[360px]">
          <h2 className="border-b border-white/10 pb-2 text-xs font-bold uppercase text-subtext1">
            Video terkait
          </h2>
          <ul className="mt-3 space-y-3">
            {sidebarRelated.map((m) => (
              <li key={m.id}>
                <Link href={`/movies/${m.id}`} className="group flex gap-2.5">
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img
                    src={m.backdrop_url ?? m.poster_url ?? thumbUrl(m.id)}
                    alt=""
                    loading="lazy"
                    className="h-[68px] w-[120px] shrink-0 rounded-sm bg-base object-cover"
                  />
                  <div className="min-w-0">
                    <p className="line-clamp-2 text-xs font-medium leading-snug text-text group-hover:text-mauve">
                      {m.title}
                    </p>
                    <p className="mt-1 text-[0.7rem] text-overlay0">{formatBytes(m.video_size)}</p>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        </aside>
      </div>

      {related.length > 0 && (
        <section className="mt-10">
          <h2 className="mb-3 flex items-baseline gap-2 text-sm font-bold uppercase text-subtext1">
            <span className="text-mauve">▸</span> Related Videos
          </h2>
          <div className="grid grid-cols-2 gap-x-2.5 gap-y-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-4">
            {related.slice(0, 20).map((m) => (
              <RelatedCard key={m.id} movie={m} />
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

function RelatedCard({ movie }: { movie: Movie }) {
  return (
    <Link href={`/movies/${movie.id}`} className="group block">
      <div className="relative aspect-video overflow-hidden rounded-sm bg-base">
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={movie.backdrop_url ?? movie.poster_url ?? thumbUrl(movie.id)}
          alt=""
          loading="lazy"
          className="absolute inset-0 h-full w-full object-cover"
        />
        <span className="absolute bottom-1 right-1 rounded-sm bg-black/85 px-1.5 py-px text-[0.7rem] text-white">
          {formatBytes(movie.video_size)}
        </span>
      </div>
      <p className="mt-1.5 line-clamp-2 text-[0.8rem] font-medium leading-snug text-text group-hover:text-mauve">
        {movie.title}
      </p>
    </Link>
  );
}