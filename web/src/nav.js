// spaNavigate moves to a same-origin path without reloading: App listens
// for popstate and renders the route. For links outside App's props.
export function spaNavigate(to) {
  window.history.pushState(null, "", to);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

// spaLink returns props for an <a> that navigates within the app.
export function spaLink(to, onNavigate) {
  return {
    href: to,
    onClick: (e) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button === 1) return;
      e.preventDefault();
      onNavigate?.();
      spaNavigate(to);
    },
  };
}

// Links between chat and backlog. The board listens for these events: the
// backlog opens the item dialog, the workspace shows (and highlights) the
// message, opening chat first if it is minimized.
export const OPEN_ITEM = "unison:open-item";
export const SHOW_MESSAGE = "unison:show-message";

function workspaceBase() {
  return window.location.pathname.match(/^(\/w\/[^/]+)/)?.[1] || "";
}

function eventLink(href, type, id) {
  return {
    href,
    onClick: (e) => {
      if (e.metaKey || e.ctrlKey || e.shiftKey || e.button === 1) return;
      e.preventDefault();
      window.dispatchEvent(new CustomEvent(type, { detail: id }));
    },
  };
}

// itemLink returns props for an <a> that opens backlog item id (/w/:ws?item=id).
export function itemLink(id) {
  return eventLink(`${workspaceBase()}?item=${id}`, OPEN_ITEM, id);
}

// messageLink returns props for an <a> that shows chat message id (/w/:ws/chat#msg-id).
export function messageLink(id) {
  return eventLink(`${workspaceBase()}/chat#msg-${id}`, SHOW_MESSAGE, id);
}
