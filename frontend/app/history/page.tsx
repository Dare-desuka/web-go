"use client";

// Same reasoning as bookmarks/page.tsx: the two-port setup that broke the
// browser-side fetch is gone, so /history fetches its catalog in the browser.
import { useEffect, useState } from "react";
import { listMoviesClient, type Movie } from "@/lib/api";
import HistoryList from "@/components/HistoryList";

export default function HistoryPage() {
  const [movies, setMovies] = useState<Movie[]>([]);
  useEffect(() => {
    listMoviesClient().then(setMovies);
  }, []);
  return <HistoryList movies={movies} />;
}