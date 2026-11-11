"use client";

import Link from "next/link";
import type { Movie } from "@/lib/api";
import { formatBytes, thumbUrl } from "@/lib/api";
import { isWatched, progressPct } from "@/lib/progress";
import { useEffect, useRef, useState } from "react";

export default function RowCard({
  movie,
  href,
  title,
  meta,
}: {
  movie: Movie;
  href: string;
  title?: string;
  meta?: string;
}) {
  const [watched, setWatched] = useState(false);
  const [pct, setPct] = useState<number | null>(null);
  const [visible, setVisible] = useState(false);
  const cardRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const w = isWatched(movie.id);
    setWatched(w);
    setPct(w ? null : progressPct(movie.id));
  }, [movie.id]);
  // Lazy-load the auto thumbnail: only fetch /api/thumbs/{id} when the card
  // enters the viewport. Without this, the home page fires 128 concurrent
  // Telegram RPCs, which flood-waits the account and drags video streaming
  // down to ~12 KB/s.
  useEffect(() => {
    const el = cardRef.current;
    if (!el) return;
    const io = new IntersectionObserver(
      ([e]) => {
        if (e.isIntersecting) {
          setVisible(true);
          io.disconnect();
        }
      },
      { rootMargin: "200px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);
  // Preview priority: backdrop artwork → poster artwork → auto thumbnail → gradient.
  const art = movie.backdrop_url ?? movie.poster_url;
  const label = title ?? movie.title;

  return (
    <Link href={href} className="group block">
      <div ref={cardRef} className="relative aspect-video overflow-hidden rounded-sm bg-base">
        {art ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={art}
            alt=""
            loading="lazy"
            className="absolute inset-0 h-full w-full object-cover"
            onError={(e) => {
              (e.target as HTMLImageElement).style.display = "none";
            }}
          />
        ) : visible ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={thumbUrl(movie.id)}
            alt=""
            loading="lazy"
            className="absolute inset-0 h-full w-full object-cover"
            onError={(e) => {
              (e.target as HTMLImageElement).style.display = "none";
            }}
          />
        ) : (
          <div className="absolute inset-0 h-full w-full bg-base" />
        )}

        {/* Hover scrim + play glyph, Pornhub-style. */}
        <div className="absolute inset-0 flex items-center justify-center bg-black/0 opacity-0 transition group-hover:bg-black/35 group-hover:opacity-100">
          <div className="flex h-9 w-9 items-center justify-center rounded-full bg-black/60 text-base text-white">
            ▶
          </div>
        </div>

        {/* Duration / status, bottom-right on the thumb. */}
        {movie.duration_seconds ? (
          <span className="absolute bottom-1 right-1 rounded-sm bg-black/85 px-1.5 py-px font-mono text-[0.7rem] text-white">
            {Math.floor(movie.duration_seconds / 60)}:
            {String(movie.duration_seconds % 60).padStart(2, "0")}
          </span>
        ) : movie.status !== "READY" ? (
          <span className="absolute bottom-1 right-1 rounded-sm bg-black/85 px-1.5 py-px font-mono text-[0.7rem] text-softred">
            {movie.status}
          </span>
        ) : null}

        {watched && (
          <span className="absolute left-1 top-1 rounded-sm bg-black/85 px-1.5 py-px text-[0.62rem] font-bold uppercase text-white">
            Ditonton
          </span>
        )}

        {pct !== null && !watched && (
          <div className="absolute bottom-0 left-0 h-[3px] w-full bg-black/60">
            <div className="h-full bg-mauve" style={{ width: `${pct}%` }} />
          </div>
        )}
      </div>

      <p className="mt-1.5 line-clamp-2 text-[0.8rem] font-medium leading-snug text-text group-hover:text-mauve" title={label}>
        {label}
      </p>
      <p className="mt-0.5 truncate text-xs text-overlay0">
        {meta ?? [movie.year, formatBytes(movie.video_size)].filter(Boolean).join(" · ")}
      </p>
    </Link>
  );
}