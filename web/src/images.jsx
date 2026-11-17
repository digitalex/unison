import { createContext, useContext, useEffect, useRef, useState } from "react";
import { ImagePlus } from "lucide-react";
import { getToken, uploadImage } from "./api.js";
import { Textarea } from "./ui/Input.jsx";

// Images in Markdown (#292): screenshots pasted, dropped or picked into a
// text box are uploaded to the workspace (POST …/images), and the box gets
// ![name](/api/workspaces/{ws}/images/{id}), which Markdown shows inline.

// ImageUpload is the current workspace's upload function (file → image), or
// null outside a workspace (where boxes stay plain textareas).
export const ImageUpload = createContext(null);

const IMAGE_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
const MAX_BYTES = 5 << 20;

// imageUploader returns the upload function for workspace ws.
export function imageUploader(ws, token) {
  return (file) => uploadImage(ws, token, file);
}

function imageFiles(list) {
  return [...(list || [])].filter((f) => f.type.startsWith("image/"));
}

// ImageTextarea is a Textarea that takes images: paste a screenshot, drop
// files on it, or pick them with its button. Each upload first inserts an
// "uploading" placeholder at the cursor, then swaps in the Markdown when it
// is done, so typing meanwhile is fine. value/onChange as for Textarea.
export function ImageTextarea({ value, onChange, className = "", ...props }) {
  const upload = useContext(ImageUpload);
  const ref = useRef(null);
  const latest = useRef(value);
  latest.current = value;
  const picker = useRef(null);
  const seq = useRef(0);
  const [error, setError] = useState("");
  const [dragging, setDragging] = useState(false);
  const textarea = props.ref || ref;

  if (!upload) return <Textarea value={value} onChange={onChange} className={className} {...props} />;

  const set = (v) => onChange({ target: { value: v } });

  const add = (files) => {
    setError("");
    for (const file of files) {
      if (!IMAGE_TYPES.includes(file.type)) {
        setError(`${file.name || "That file"} isn't a PNG, JPEG, GIF or WebP image.`);
        continue;
      }
      if (file.size > MAX_BYTES) {
        setError(`${file.name || "That image"} is over 5 MiB.`);
        continue;
      }
      const placeholder = `![Uploading ${file.name || "image"}… #${++seq.current}]()`;
      const el = textarea.current;
      const cur = latest.current;
      const at = el && document.activeElement === el ? el.selectionStart : cur.length;
      const before = cur.slice(0, at);
      const sep = before && !/\s$/.test(before) ? "\n" : "";
      set(before + sep + placeholder + cur.slice(at));
      upload(file).then(
        (im) => set(latest.current.replace(placeholder, im.markdown)),
        (e) => {
          set(latest.current.replace(placeholder, ""));
          setError(`Couldn't upload ${file.name || "the image"}: ${e.message}`);
        },
      );
    }
  };

  return (
    <div className={dragging ? "image-textarea dragging" : "image-textarea"}>
      <Textarea
        {...props}
        ref={textarea}
        className={className}
        value={value}
        onChange={onChange}
        onPaste={(e) => {
          const files = imageFiles(e.clipboardData?.files);
          if (files.length) {
            e.preventDefault();
            add(files);
          }
          props.onPaste?.(e);
        }}
        onDragOver={(e) => {
          if ([...(e.dataTransfer?.types || [])].includes("Files")) {
            e.preventDefault();
            setDragging(true);
          }
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          setDragging(false);
          const files = [...(e.dataTransfer?.files || [])];
          if (files.length) {
            e.preventDefault();
            add(files);
          }
        }}
      />
      <button
        type="button"
        className="image-attach"
        aria-label="Add an image"
        title="Add an image (or paste / drop one)"
        onClick={() => picker.current?.click()}
      >
        <ImagePlus aria-hidden="true" />
      </button>
      <input
        ref={picker}
        type="file"
        accept={IMAGE_TYPES.join(",")}
        multiple
        hidden
        onChange={(e) => {
          add([...e.target.files]);
          e.target.value = "";
        }}
      />
      {error && (
        <p role="alert" className="image-error">
          {error}
        </p>
      )}
    </div>
  );
}

// IMAGE_PATH matches the images Markdown may show: this server's workspace
// images only, so a message can't make readers' browsers fetch other URLs.
const IMAGE_PATH = /^\/api\/workspaces\/([\w-]+)\/images\/(\d+)$/;

export function imagePath(url) {
  const m = url.trim().match(IMAGE_PATH);
  return m ? { path: m[0], ws: m[1] } : null;
}

// MdImage shows a workspace image, linking to it full size. Signed in, the
// session cookie loads it; with a participant token instead (cookies don't
// carry it), it is fetched with the token and shown from a blob URL.
export function MdImage({ path, ws, alt }) {
  const token = getToken(ws);
  const [src, setSrc] = useState(token ? "" : path);
  useEffect(() => {
    if (!token) return;
    let url = "";
    let current = true;
    fetch(path, { headers: { Authorization: `Bearer ${token}` } })
      .then((r) => (r.ok ? r.blob() : Promise.reject()))
      .then((b) => {
        if (!current) return;
        url = URL.createObjectURL(b);
        setSrc(url);
      })
      .catch(() => {});
    return () => {
      current = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [path, token]);
  if (!src) return <span className="md-img md-img-loading">{alt || "image"}</span>;
  return (
    <a className="md-img-link" href={src} target="_blank" rel="noopener noreferrer">
      <img className="md-img" src={src} alt={alt} loading="lazy" />
    </a>
  );
}
