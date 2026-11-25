import { useEffect, useRef, useState } from "react";
import { Bug, Camera, Crop, Lightbulb, RotateCcw, Trash2, Upload, X } from "lucide-react";
import { uploadImage } from "./api.js";
import { submitOnModEnter } from "./backlogFormat.js";
import { MentionTextarea } from "./mentions.jsx";
import Button from "./ui/Button.jsx";
import Dialog from "./ui/Dialog.jsx";
import Field from "./ui/Field.jsx";
import { Input } from "./ui/Input.jsx";

const IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const MAX_IMAGE_BYTES = 5 << 20;

export const FEEDBACK_KINDS = [
  { id: "bug", label: "Bug", Icon: Bug },
  { id: "suggestion", label: "Suggestion", Icon: Lightbulb },
];

export const FEEDBACK_SEVERITIES = [
  { id: "low", label: "Low" },
  { id: "medium", label: "Medium" },
  { id: "high", label: "High" },
  { id: "critical", label: "Critical" },
];

function clamp01(v) {
  return Math.max(0, Math.min(1, v));
}

function loadImageDimensions(url) {
  return new Promise((resolve, reject) => {
    const img = new window.Image();
    img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight, img });
    img.onerror = () => reject(new Error("Could not read image."));
    img.src = url;
  });
}

async function sliceImageBlob(sourceUrl, rect) {
  const { width, height, img } = await loadImageDimensions(sourceUrl);
  const sx = Math.round(rect.x * width);
  const sy = Math.round(rect.y * height);
  const sw = Math.max(1, Math.min(width - sx, Math.round(rect.w * width)));
  const sh = Math.max(1, Math.min(height - sy, Math.round(rect.h * height)));
  const canvas = document.createElement("canvas");
  canvas.width = sw;
  canvas.height = sh;
  const ctx = canvas.getContext("2d");
  ctx.drawImage(img, sx, sy, sw, sh, 0, 0, sw, sh);
  const blob = await new Promise((resolve, reject) => {
    canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("Failed to encode cropped PNG."))), "image/png");
  });
  return { blob, width: sw, height: sh };
}

function nextFrame() {
  return new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
}

// FeedbackDialog (#281): built-in Send feedback modal with Bug/Suggestion
// toggle, severity selector, tab screenshot capture + interactive drag-crop
// (with paste and file-upload fallback), and current route context.
export default function FeedbackDialog({ workspaceId, token, call, onClose, onSubmitted }) {
  const [kind, setKind] = useState("bug");
  const [severity, setSeverity] = useState("medium");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [pageUrl] = useState(() => `${window.location.pathname}${window.location.search}`);

  // shot: { blob, url, width, height, originalBlob, originalUrl, originalWidth, originalHeight }
  const [shot, setShot] = useState(null);
  const [cropRect, setCropRect] = useState(null); // { x, y, w, h } in [0..1]
  const [capturing, setCapturing] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");

  const urlsRef = useRef(new Set());
  const fileInputRef = useRef(null);
  const imgRef = useRef(null);
  const dragOriginRef = useRef(null);

  const trackUrl = (u) => {
    urlsRef.current.add(u);
    return u;
  };

  useEffect(
    () => () => {
      for (const u of urlsRef.current) URL.revokeObjectURL(u);
      urlsRef.current.clear();
    },
    [],
  );

  const canCaptureTab =
    typeof navigator !== "undefined" && Boolean(navigator.mediaDevices?.getDisplayMedia);

  const attachFile = async (file) => {
    if (!file) return;
    setError("");
    if (file.type && !IMAGE_TYPES.includes(file.type)) {
      setError("Please choose a PNG, JPEG, GIF, or WebP image.");
      return;
    }
    if (file.size > MAX_IMAGE_BYTES) {
      setError("Screenshots can be at most 5 MiB.");
      return;
    }
    const url = trackUrl(URL.createObjectURL(file));
    try {
      const { width, height } = await loadImageDimensions(url);
      setCropRect(null);
      setShot({
        blob: file,
        url,
        width,
        height,
        originalBlob: file,
        originalUrl: url,
        originalWidth: width,
        originalHeight: height,
      });
    } catch (err) {
      setError(err.message || "Could not load that image.");
    }
  };

  const captureTab = async () => {
    if (!canCaptureTab || capturing) return;
    setError("");
    setCapturing(true);
    let stream = null;
    try {
      await nextFrame();
      stream = await navigator.mediaDevices.getDisplayMedia({
        video: { displaySurface: "browser" },
        audio: false,
        preferCurrentTab: true,
      });
      const video = document.createElement("video");
      video.srcObject = stream;
      video.muted = true;
      video.playsInline = true;
      await video.play();
      await nextFrame();
      const width = video.videoWidth || 1280;
      const height = video.videoHeight || 720;
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      canvas.getContext("2d").drawImage(video, 0, 0, width, height);
      stream.getTracks().forEach((t) => t.stop());
      stream = null;
      const blob = await new Promise((resolve, reject) => {
        canvas.toBlob((b) => (b ? resolve(b) : reject(new Error("Could not capture screenshot."))), "image/png");
      });
      await attachFile(new File([blob], "screenshot.png", { type: "image/png" }));
    } catch (err) {
      if (err?.name !== "NotAllowedError" && err?.name !== "AbortError") {
        setError("Couldn't capture this tab automatically — paste a screenshot (⌘V / Ctrl+V) or upload an image.");
      }
    } finally {
      if (stream) stream.getTracks().forEach((t) => t.stop());
      setCapturing(false);
    }
  };

  const applyCrop = async () => {
    if (!shot || !cropRect || cropRect.w < 0.02 || cropRect.h < 0.02) return;
    setError("");
    try {
      const { blob, width, height } = await sliceImageBlob(shot.url, cropRect);
      if (blob.size > MAX_IMAGE_BYTES) {
        setError("Cropped image exceeds 5 MiB.");
        return;
      }
      const url = trackUrl(URL.createObjectURL(blob));
      setShot((prev) => ({ ...prev, blob, url, width, height }));
      setCropRect(null);
    } catch (err) {
      setError(err.message || "Could not crop screenshot.");
    }
  };

  const resetCrop = () => {
    if (!shot) return;
    setCropRect(null);
    setShot((prev) => ({
      ...prev,
      blob: prev.originalBlob,
      url: prev.originalUrl,
      width: prev.originalWidth,
      height: prev.originalHeight,
    }));
  };

  const removeShot = () => {
    setCropRect(null);
    setShot(null);
  };

  const relativePoint = (e) => {
    const el = imgRef.current;
    if (!el) return null;
    const r = el.getBoundingClientRect();
    if (!r.width || !r.height) return null;
    return {
      x: clamp01((e.clientX - r.left) / r.width),
      y: clamp01((e.clientY - r.top) / r.height),
    };
  };

  const onCropPointerDown = (e) => {
    if (e.button !== 0) return;
    const pt = relativePoint(e);
    if (!pt) return;
    e.preventDefault();
    e.currentTarget.setPointerCapture?.(e.pointerId);
    dragOriginRef.current = pt;
    setCropRect({ x: pt.x, y: pt.y, w: 0, h: 0 });
  };

  const onCropPointerMove = (e) => {
    const start = dragOriginRef.current;
    if (!start) return;
    const pt = relativePoint(e);
    if (!pt) return;
    setCropRect({
      x: Math.min(start.x, pt.x),
      y: Math.min(start.y, pt.y),
      w: Math.abs(pt.x - start.x),
      h: Math.abs(pt.y - start.y),
    });
  };

  const onCropPointerUp = () => {
    if (!dragOriginRef.current) return;
    dragOriginRef.current = null;
    setCropRect((cur) => (cur && cur.w >= 0.02 && cur.h >= 0.02 ? cur : null));
  };

  const onPasteCapture = (e) => {
    const files = [...(e.clipboardData?.files || [])].filter((f) => f.type.startsWith("image/"));
    if (!files.length) return;
    e.preventDefault();
    e.stopPropagation();
    attachFile(files[0]);
  };

  const submit = async (e) => {
    e.preventDefault();
    if (submitting || !title.trim()) return;
    setError("");
    setSubmitting(true);
    try {
      let imageURL = "";
      if (shot) {
        let finalBlob = shot.blob;
        if (cropRect && cropRect.w >= 0.02 && cropRect.h >= 0.02) {
          const sliced = await sliceImageBlob(shot.url, cropRect);
          finalBlob = sliced.blob;
        }
        const file = new File([finalBlob], "feedback-screenshot.png", {
          type: finalBlob.type || "image/png",
        });
        const uploaded = await uploadImage(workspaceId, token, file);
        imageURL = uploaded.url;
      }
      const created = await call("POST", "/feedback", {
        kind,
        severity,
        title: title.trim(),
        body: body.trim(),
        image_url: imageURL,
        page_url: pageUrl,
      });
      onSubmitted?.(created);
      onClose();
    } catch (err) {
      setError(err.message || "Failed to send feedback.");
      setSubmitting(false);
    }
  };

  const hasActiveCrop = Boolean(cropRect && cropRect.w >= 0.02 && cropRect.h >= 0.02);
  const isCropped = Boolean(shot && shot.url !== shot.originalUrl);
  const cropPixelLabel =
    shot && hasActiveCrop
      ? `${Math.max(1, Math.round(cropRect.w * shot.width))} × ${Math.max(1, Math.round(cropRect.h * shot.height))} px`
      : "";

  return (
    <Dialog
      title="Send feedback"
      onClose={onClose}
      className={capturing ? "item-dialog feedback-dialog feedback-dialog-capturing" : "item-dialog feedback-dialog"}
    >
      <form className="form feedback-form" onSubmit={submit} onKeyDown={submitOnModEnter} onPasteCapture={onPasteCapture}>
        {error && (
          <p role="alert" className="error-bar">
            {error}
          </p>
        )}

        <div className="feedback-controls-row">
          <Field label="Type">
            <div className="seg feedback-seg" role="group" aria-label="Feedback type">
              {FEEDBACK_KINDS.map(({ id, label, Icon }) => (
                <button key={id} type="button" aria-pressed={kind === id} onClick={() => setKind(id)}>
                  <Icon aria-hidden="true" />
                  {label}
                </button>
              ))}
            </div>
          </Field>

          <Field label="Severity">
            <div className="seg feedback-seg" role="group" aria-label="Severity">
              {FEEDBACK_SEVERITIES.map(({ id, label }) => (
                <button key={id} type="button" aria-pressed={severity === id} onClick={() => setSeverity(id)}>
                  {label}
                </button>
              ))}
            </div>
          </Field>
        </div>

        <Field label="Title">
          <Input
            required
            data-autofocus
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder={kind === "bug" ? "What broke or felt off?" : "What would make this better?"}
          />
        </Field>

        <Field label="Details">
          <MentionTextarea
            rows={4}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="Steps to reproduce, expected behavior, or context… (Markdown)"
          />
        </Field>

        <div className="field feedback-shot-field">
          <div className="feedback-shot-head">
            <span className="field-label">Screenshot</span>
            <span className="muted feedback-shot-hint">
              {shot ? "Drag over the image to crop" : "Capture tab, upload, or paste (⌘V / Ctrl+V)"}
            </span>
          </div>

          {!shot ? (
            <div className="feedback-shot-empty">
              <div className="actions-row feedback-shot-buttons">
                {canCaptureTab && (
                  <Button type="button" variant="secondary" onClick={captureTab} disabled={capturing}>
                    <Camera aria-hidden="true" />
                    {capturing ? "Capturing…" : "Capture tab"}
                  </Button>
                )}
                <Button type="button" variant="secondary" onClick={() => fileInputRef.current?.click()}>
                  <Upload aria-hidden="true" />
                  Upload image
                </Button>
              </div>
            </div>
          ) : (
            <div className="feedback-shot-stage">
              <div
                className="feedback-crop-viewport"
                onPointerDown={onCropPointerDown}
                onPointerMove={onCropPointerMove}
                onPointerUp={onCropPointerUp}
                onPointerCancel={onCropPointerUp}
              >
                <img
                  ref={imgRef}
                  src={shot.url}
                  alt="Attached screenshot preview"
                  className="feedback-crop-img"
                  draggable={false}
                />
                {cropRect && (cropRect.w > 0 || cropRect.h > 0) && (
                  <div
                    className="feedback-crop-box"
                    style={{
                      left: `${cropRect.x * 100}%`,
                      top: `${cropRect.y * 100}%`,
                      width: `${cropRect.w * 100}%`,
                      height: `${cropRect.h * 100}%`,
                    }}
                  >
                    {cropPixelLabel && <span className="feedback-crop-size">{cropPixelLabel}</span>}
                  </div>
                )}
              </div>

              <div className="actions-row feedback-crop-toolbar">
                {hasActiveCrop ? (
                  <>
                    <Button type="button" size="small" variant="primary" onClick={applyCrop}>
                      <Crop aria-hidden="true" />
                      Crop selection
                    </Button>
                    <Button type="button" size="small" variant="ghost" onClick={() => setCropRect(null)}>
                      <X aria-hidden="true" />
                      Clear selection
                    </Button>
                  </>
                ) : (
                  <span className="muted feedback-shot-dims">
                    {shot.width} × {shot.height} px{isCropped ? " (cropped)" : ""}
                  </span>
                )}
                <span className="spacer" />
                {isCropped && (
                  <Button type="button" size="small" variant="ghost" onClick={resetCrop}>
                    <RotateCcw aria-hidden="true" />
                    Reset
                  </Button>
                )}
                <Button type="button" size="small" variant="ghost" onClick={() => fileInputRef.current?.click()}>
                  <Upload aria-hidden="true" />
                  Replace
                </Button>
                <Button type="button" size="small" variant="ghost" className="button-danger" onClick={removeShot}>
                  <Trash2 aria-hidden="true" />
                  Remove
                </Button>
              </div>
            </div>
          )}

          <input
            ref={fileInputRef}
            type="file"
            accept={IMAGE_TYPES.join(",")}
            hidden
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) attachFile(file);
              e.target.value = "";
            }}
          />
        </div>

        <div className="feedback-foot">
          <span className="muted feedback-page-context" title={pageUrl}>
            Page: <code>{pageUrl}</code>
          </span>
          <div className="actions-row dialog-actions">
            <Button type="button" variant="ghost" onClick={onClose} disabled={submitting}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={submitting || !title.trim()}>
              {submitting ? "Sending…" : "Send feedback"}
            </Button>
          </div>
        </div>
      </form>
    </Dialog>
  );
}
