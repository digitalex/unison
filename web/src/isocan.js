// Making an isocan canvas from Unison (#297). isocan's creation page, opened
// in a popup as /?new=<name>&opener=<our origin>, makes the canvas and posts
// {type: "isocan:created", url} back to the window that opened it.

export const ISOCAN_ORIGIN = "https://isocan.io";

const CANVAS_URL = /^https:\/\/isocan\.io\/p\/[A-Za-z0-9_-]+$/;

export function isCanvasUrl(url) {
  return typeof url === "string" && CANVAS_URL.test(url);
}

// createCanvas opens the popup and resolves with the new canvas's URL. Call
// it synchronously from a click or submit handler: browsers block popups
// opened later. It rejects, with a message for the person, when the popup is
// blocked, is closed before a canvas exists, or answers with anything but a
// canvas URL. Messages from other origins or windows are ignored.
export function createCanvas(name) {
  const target = `${ISOCAN_ORIGIN}/?new=${encodeURIComponent(name)}&opener=${encodeURIComponent(window.location.origin)}`;
  const popup = window.open(target, "isocan-new-canvas", "popup,width=960,height=720");
  if (!popup) {
    return Promise.reject(new Error("Your browser blocked the isocan window. Allow pop-ups for this site and try again, or paste a canvas URL instead."));
  }
  return new Promise((resolve, reject) => {
    const finish = (settle, value) => {
      window.removeEventListener("message", onMessage);
      clearInterval(watch);
      settle(value);
    };
    const onMessage = (e) => {
      if (e.origin !== ISOCAN_ORIGIN || e.source !== popup || e.data?.type !== "isocan:created") return;
      popup.close();
      if (isCanvasUrl(e.data.url)) finish(resolve, e.data.url);
      else finish(reject, new Error("isocan didn't answer with a canvas URL. Paste one instead, or leave it empty and add it in Settings later."));
    };
    const watch = setInterval(() => {
      if (popup.closed) finish(reject, new Error("The isocan window was closed before it made a canvas. Try again, or paste a canvas URL instead."));
    }, 400);
    window.addEventListener("message", onMessage);
  });
}
