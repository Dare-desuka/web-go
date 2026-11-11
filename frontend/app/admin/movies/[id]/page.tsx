import AdminMovieView from "@/components/AdminMovieView";

// ponytail: server wrapper for generateStaticParams only. The view resolves
// the movie id from the URL in the browser, so one shell serves every id.
export function generateStaticParams() {
  return [{ id: "_" }];
}

export default function Page() {
  return <AdminMovieView />;
}