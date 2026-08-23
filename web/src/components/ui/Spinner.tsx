/** A quiet placeholder while something is on its way. */
export function Spinner({ label = "Loading" }: { label?: string }) {
  return (
    <p className="py-12 text-center text-sm text-ink-faint" role="status">
      {label}…
    </p>
  );
}
