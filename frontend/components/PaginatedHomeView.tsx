"use client";

import { useEffect, useState } from "react";
import { notFound, useParams } from "next/navigation";
import { listMoviesClient, type Movie } from "@/lib/api";
import HomeApp from "@/components/HomeApp";

// Paginated home: /2, /3, … Riwayat is home-only (showTop), so pages 2+ are
// just the catalog grid + pagination.
//
// The page number is read from the URL in the browser, not at build time: the
// static export emits one shell for the whole /[page] family and Go serves it
// for every /N. See app/[page]/page.tsx.
export default function PaginatedHomeView() {
  const routeParams = useParams<{ page: string }>();
  const [movies, setMovies] = useState<Movie[] | null>(null);
  const [page, setPage] = useState(0);

  useEffect(() => {
    const resolved = routeParams?.page;
    const n = Number(resolved && resolved !== "2" ? resolved : window.location.pathname.match(/^\/(\d+)/)?.[1]);
    if (!Number.isInteger(n) || n < 2) {
      notFound();
      return;
    }
    setPage(n);
    listMoviesClient().then(setMovies);
  }, [routeParams]);

  if (!movies) return null;
  return <HomeApp movies={movies} page={page} showTop={false} />;
}