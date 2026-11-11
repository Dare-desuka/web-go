import PaginatedHomeView from "@/components/PaginatedHomeView";

// ponytail: server wrapper for generateStaticParams only. The view reads the
// page number from the URL in the browser, so one shell serves every /N.
export function generateStaticParams() {
  return [{ page: "2" }];
}

export default function Page() {
  return <PaginatedHomeView />;
}