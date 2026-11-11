import MovieDetailView from "@/components/MovieDetailView";

// ponytail: the page stays a server component only to carry
// generateStaticParams, which Next forbids inside "use client" files. One
// sentinel id is enough: MovieDetailView resolves the real id from the URL in
// the browser, so every /movies/<id> needs the same shell. Go maps any
// /movies/<id> request to this file.
export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function Page() {
  return <MovieDetailView />;
}