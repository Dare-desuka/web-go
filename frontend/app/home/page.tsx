"use client";

import { useEffect, useState } from "react";
import { listMoviesClient, type Movie } from "@/lib/api";
import HomeApp from "@/components/HomeApp";

export default function Home() {
  const [movies, setMovies] = useState<Movie[]>([]);
  useEffect(() => {
    listMoviesClient().then(setMovies);
  }, []);
  return <HomeApp movies={movies} page={1} showTop />;
}