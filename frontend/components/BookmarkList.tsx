"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { thumbUrl, type Movie } from "@/lib/api";
import { listAll, me } from "@/lib/admin";
import { bookmarkedMovies, removeBookmark } from "@/lib/bookmark";

// Same shape as HistoryList: server-supplied catalog, admin swaps in listAll()
// so bookmarked DRAFT films survive. Browser-side listMoviesClient() silently
// returned [] and dropped them. ponytail: fixed by deleting the failing hop.
export default function BookmarkList({ movies }: { movies: Movie[] }) {
  const router = useRouter();
  const [catalog, setCatalog] = useState(movies);
  const [list, setList] = useState<Movie[] | null>(null);

  useEffect(() => {
    me()
      .then((u) => {
        if (u && u.role === "admin") listAll().then(setCatalog).catch(() => {});
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    setList(bookmarkedMovies(catalog));
  }, [catalog]);

  if (list === null) return <p className="mt-10 text-subtext0">Memuat…</p>;

  return (
    <div className="mt-6">
      <h1 className="text-2xl font-bold">Bookmark</h1>
      {list.length === 0 ? (
        <p className="mt-4 text-subtext0">
          Belum ada bookmark. Tandai ☆ di halaman film untuk menyimpan.
        </p>
      ) : (
        <ul className="mt-4 flex flex-col gap-2">
          {list.map((m) => (
            <li
              key={m.id}
              className="flex items-center gap-3 rounded-sm border border-white/10 bg-mantle p-3"
            >
              <button onClick={() => router.push(`/movies/${m.id}`)} className="shrink-0">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={m.backdrop_url ?? m.poster_url ?? thumbUrl(m.id)}
                  alt=""
                  loading="lazy"
                  className="aspect-video w-36 rounded-sm object-cover"
                />
              </button>
              <div className="min-w-0 flex-1">
                <Link href={`/movies/${m.id}`} className="truncate font-medium hover:text-mauve">
                  {m.title}
                </Link>
                {m.year && <p className="font-mono text-xs text-overlay0">{m.year}</p>}
              </div>
              <button
                onClick={() => {
                  removeBookmark(m.id);
                  setList(bookmarkedMovies(catalog));
                }}
                aria-label={`Hapus bookmark ${m.title}`}
                className="shrink-0 rounded-sm border border-white/10 px-3 py-1.5 text-sm text-softred hover:bg-softred/10"
              >
                Hapus
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}