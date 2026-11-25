import { useEffect, useRef, useState } from "react";
import { Check, Link2 } from "lucide-react";
import Button from "./Button.jsx";

const COPIED_FEEDBACK_MS = 1500;

export function resolveAbsoluteUrl(
  href,
  origin = typeof window !== "undefined" ? window.location?.origin : "http://localhost",
) {
  try {
    return new URL(href, origin || "http://localhost").href;
  } catch {
    return String(href ?? "");
  }
}

function fallbackCopyText(text) {
  if (typeof document === "undefined" || !document.body) return false;
  const area = document.createElement("textarea");
  area.value = text;
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.top = "-9999px";
  area.style.opacity = "0";
  area.style.pointerEvents = "none";
  document.body.appendChild(area);
  try {
    area.select();
    return Boolean(document.execCommand?.("copy"));
  } catch {
    return false;
  } finally {
    area.remove();
  }
}

export async function copyTextToClipboard(text) {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // Fall through to textarea copy fallback when clipboard permission fails.
    }
  }
  return fallbackCopyText(text);
}

export default function CopyLinkButton({
  href,
  label = "Copy link",
  title = "Copy link",
  className = "",
}) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef(null);

  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    },
    [],
  );

  const onCopy = async (e) => {
    e.stopPropagation();
    const url = resolveAbsoluteUrl(href);
    const ok = await copyTextToClipboard(url);
    if (!ok) return;
    setCopied(true);
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      timerRef.current = null;
      setCopied(false);
    }, COPIED_FEEDBACK_MS);
  };

  const classes = ["copy-link-btn", copied && "copied", className].filter(Boolean).join(" ");
  return (
    <Button
      type="button"
      iconOnly
      variant="ghost"
      className={classes}
      aria-label={copied ? "Copied link" : label}
      title={copied ? "Copied link" : title}
      data-copied={copied ? "true" : undefined}
      onClick={onCopy}
    >
      {copied ? <Check aria-hidden="true" /> : <Link2 aria-hidden="true" />}
    </Button>
  );
}
