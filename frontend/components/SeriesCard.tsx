"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import type { SeriesGroup } from "@/lib/series";
import { formatBytes, thumbUrl } from "@/lib/api";

export default function SeriesCard({ group }: { group: SeriesGroup }) {
  const cover = group.movies[0];
  const art = cover.backdrop_url ?? cover.poster_url;
  const href = group.movies.length === 1 ? `/movies/${cover.id}` : `/series/${group.key}`;
  const count = group.movies.length;
  // Singleton keeps its own title; series shows the base label.
  const title = count === 1 && group.key.startsWith("__single:") ? cover.title : group.label;

  const [visible, setVisible] = useState(false);
  const cardRef = useRef<HTMLDivElement>(null);
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
            src={thumbUrl(cover.id)}
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

        <div className="absolute inset-0 flex items-center justify-center bg-black/0 opacity-0 transition group-hover:bg-black/35 group-hover:opacity-100">
          <div className="flex h-9 w-9 items-center justify-center rounded-full bg-black/60 text-base text-white">
            ▶
          </div>
        </div>

        {cover.status !== "READY" ? (
          <span className="absolute bottom-1 right-1 rounded-sm bg-black/85 px-1.5 py-px font-mono text-[0.7rem] text-softred">
            {cover.status}
          </span>
        ) : count > 1 ? (
          <span className="absolute bottom-1 right-1 rounded-sm bg-black/85 px-1.5 py-px text-[0.7rem] text-white">
            {count} Eps
          </span>
        ) : null}
      </div>

      <p className="mt-1.5 line-clamp-2 text-[0.8rem] font-medium leading-snug text-text group-hover:text-mauve" title={title}>
        {title}
      </p>
      <p className="mt-0.5 truncate text-xs text-overlay0">
        {[cover.year, count > 1 ? formatBytes(group.movies.reduce((a, m) => a + m.video_size, 0)) : null]
          .filter(Boolean)
          .join(" · ")}
      </p>
    </Link>
  );
}