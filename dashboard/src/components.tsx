import { Check, CheckCheck, Copy, ExternalLink } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { AnchorHTMLAttributes, ReactNode } from "react";
import type { Health } from "./types";

export function BrandMark() {
  return (
    <svg
      className="brand-mark"
      viewBox="0 0 40 40"
      fill="none"
      aria-hidden="true"
    >
      <rect width="40" height="40" rx="11" fill="currentColor" />
      <g stroke="#28382b" strokeWidth="2.3" strokeLinejoin="round">
        <path d="m20 8 11 6.5v12L20 33 9 26.5v-12L20 8Z" />
        <path d="m9 14.5 11 6.3 11-6.3M20 21v12M14.5 11.3l11 6.4v5.9" />
      </g>
    </svg>
  );
}

const healthLabels: Record<Health, string> = {
  healthy: "Healthy",
  degraded: "Needs attention",
  unknown: "Unknown",
};

export function Status({
  health,
  label,
  subtle = false,
}: {
  health: Health;
  label?: string;
  subtle?: boolean;
}) {
  return (
    <span
      className={`status status-${health}${subtle ? " status-subtle" : ""}`}
    >
      <span className="status-dot" />
      {label ?? healthLabels[health]}
    </span>
  );
}

export function ExternalLinkButton({
  children,
  className = "",
  ...props
}: AnchorHTMLAttributes<HTMLAnchorElement>) {
  return (
    <a
      {...props}
      className={className}
      target="_blank"
      rel="noopener noreferrer"
    >
      {children}
      <ExternalLink size={14} aria-hidden="true" />
      <span className="sr-only"> (opens in a new tab)</span>
    </a>
  );
}

export function CopyButton({
  text,
  label = "Copy",
  compact = false,
}: {
  text: string;
  label?: string;
  compact?: boolean;
}) {
  const [result, setResult] = useState<"idle" | "copied" | "failed">("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);

  async function copy() {
    clearTimeout(timer.current);
    try {
      await navigator.clipboard.writeText(text);
      setResult("copied");
    } catch {
      setResult("failed");
    }
    timer.current = setTimeout(() => setResult("idle"), 3500);
  }

  return (
    <span className="copy-control">
      <button
        className={
          compact ? "icon-button copy-button" : "button button-secondary"
        }
        onClick={() => void copy()}
        aria-label={result === "copied" ? "Copied" : label}
        title={result === "copied" ? "Copied" : label}
      >
        {result === "copied" ? <CheckCheck size={15} /> : <Copy size={15} />}
        {!compact && (result === "copied" ? "Copied" : label)}
      </button>
      <span
        className={result === "failed" ? "copy-error" : "sr-only"}
        role="status"
      >
        {result === "copied"
          ? "Copied to clipboard."
          : result === "failed"
            ? "Copy unavailable. Select and copy the text."
            : ""}
      </span>
    </span>
  );
}

export function CodeBlock({
  code,
  numbered = false,
}: {
  code: string;
  numbered?: boolean;
}) {
  return (
    <pre
      className={`code-block${numbered ? " code-numbered" : ""}`}
      tabIndex={0}
      aria-label={numbered ? "YAML configuration" : "CLI commands"}
    >
      <code>
        {code
          .trimEnd()
          .split("\n")
          .map((line, index) => {
            const match = /^(\s*[\w]+:)(.*)$/.exec(line);
            return (
              <span className="code-line" key={index}>
                {numbered && (
                  <span className="line-number" aria-hidden="true">
                    {index + 1}
                  </span>
                )}
                <span>
                  {match ? (
                    <>
                      <span className="yaml-key">{match[1]}</span>
                      <span className="yaml-value">{match[2]}</span>
                    </>
                  ) : (
                    line
                  )}
                </span>
                {"\n"}
              </span>
            );
          })}
      </code>
    </pre>
  );
}

export function SectionHeading({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children?: ReactNode;
}) {
  return (
    <div className="section-heading">
      <div>
        <h2>{title}</h2>
        {description && <p>{description}</p>}
      </div>
      {children}
    </div>
  );
}

export function CheckItem({ children }: { children: ReactNode }) {
  return (
    <span className="check-item">
      <Check size={14} aria-hidden="true" />
      {children}
    </span>
  );
}
