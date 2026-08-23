import type { ButtonHTMLAttributes, ReactNode } from "react";

type Variant = "primary" | "quiet" | "ghost" | "danger";

const base =
  "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium " +
  "transition-colors disabled:opacity-50 disabled:pointer-events-none " +
  "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent";

const variants: Record<Variant, string> = {
  primary: "bg-ink text-paper hover:bg-ink/85 px-3.5 py-2",
  quiet:
    "border border-rule bg-paper-raised text-ink hover:bg-paper-sunken px-3.5 py-2",
  ghost: "text-ink-muted hover:text-ink hover:bg-paper-sunken px-2.5 py-1.5",
  danger: "border border-rule text-accent hover:bg-accent/10 px-3.5 py-2",
};

export function Button({
  variant = "quiet",
  className = "",
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: Variant;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      className={`${base} ${variants[variant]} ${className}`}
      {...rest}
    >
      {children}
    </button>
  );
}
