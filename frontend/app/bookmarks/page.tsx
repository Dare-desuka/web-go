"use client";

// Catalog is fetched in the browser now. It used to be fetched server-side
// because the browser could not always reach the API port when Next ran on
// 3003 and Go on 3004. Go now serves this same bundle from the API's own
// origin, so that failure mode is gone and admin-only DRAFT films still
// reach BookmarkList through its own listAll() swap.
import { useEffect, useState } from "react";
import { listMoviesClient, type Movie } from "@/lib/api";
import BookmarkList from "@/components/BookmarkList";

export default function BookmarksPage() {
  const [movies, setMovies] = useState<Movie[]>([]);
  useEffect(() => {
    listMoviesClient().then(setMovies);
  }, []);
  return <BookmarkList movies={movies} />;
}