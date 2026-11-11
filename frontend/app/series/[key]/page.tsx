import SeriesPageView from "@/components/SeriesPageView";

// ponytail: server wrapper for generateStaticParams only. The view resolves
// the series key from the URL in the browser, so one shell serves every key.
export function generateStaticParams() {
  return [{ key: "_" }];
}

export default function Page() {
  return <SeriesPageView />;
}