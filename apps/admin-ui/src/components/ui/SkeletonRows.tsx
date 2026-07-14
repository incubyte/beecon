export interface SkeletonRowsProps {
  rows?: number;
  columns?: number;
}

/** SkeletonRows renders placeholder table rows while a list query is
 * loading (DESIGN.md §7): shimmer respects prefers-reduced-motion via the
 * `animate-pulse` utility, which Tailwind itself already gates behind the
 * media query at the browser level. */
export function SkeletonRows({ rows = 5, columns = 3 }: SkeletonRowsProps) {
  return (
    <>
      {Array.from({ length: rows }).map((_, rowIndex) => (
        <tr key={rowIndex} aria-hidden="true">
          {Array.from({ length: columns }).map((_, columnIndex) => (
            <td key={columnIndex} className="px-4 py-3">
              <div className="h-4 w-full max-w-48 animate-pulse rounded bg-surface-muted motion-reduce:animate-none" />
            </td>
          ))}
        </tr>
      ))}
    </>
  );
}
