// Instant placeholder while /home or /N awaits its server fetch, so paging
// doesn't freeze on the old page. Same grid shape as HomeApp.
export default function HomeSkeleton() {
  return (
    <div className="animate-pulse">
      <div className="mt-4 h-9 border-y border-white/10 bg-crust" />
      <div className="mt-8 h-4 w-40 rounded-sm bg-white/10" />
      <div className="mt-3 grid grid-cols-2 gap-x-2.5 gap-y-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-4">
        {Array.from({ length: 12 }).map((_, i) => (
          <div key={i}>
            <div className="aspect-video rounded-sm bg-base" />
            <div className="mt-2 h-3 w-3/4 rounded-sm bg-white/10" />
            <div className="mt-1 h-3 w-1/3 rounded-sm bg-white/10" />
          </div>
        ))}
      </div>
    </div>
  );
}
